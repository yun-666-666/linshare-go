package net.linshare.go;

import android.app.*;
import android.content.*;
import android.database.Cursor;
import android.net.Uri;
import android.os.Environment;
import android.provider.MediaStore;
import android.util.AtomicFile;
import java.io.*;
import java.net.*;
import java.util.*;
import java.util.concurrent.Executors;
import java.util.concurrent.ExecutorService;
import org.json.*;

/** App-owned transfers. Completed records survive service and phone restarts. */
public final class AppDownloads {
 private static final String HOME="http://127.0.0.1:48773";
 private static AppDownloads instance;
 private final Context context;
 private final AtomicFile ledger;
 private final ArrayList<Job> jobs=new ArrayList<>();
 private final ExecutorService worker=Executors.newSingleThreadExecutor();
 private int nextId=100;
 private String storageError="";

 private static final class Job {
  int id; String key,url,name,kind,status="queued",uri="",mime="application/octet-stream",error="";
  long bytes,total=-1,retryAt;
  JSONObject json() throws JSONException {
   return new JSONObject().put("id",id).put("key",key).put("url",url).put("name",name).put("kind",kind)
    .put("status",status).put("uri",uri).put("mime",mime).put("error",error).put("bytes",bytes).put("total",total).put("retryAt",retryAt);
  }
  Job(JSONObject value) throws JSONException {
   id=value.getInt("id");key=value.getString("key");url=value.getString("url");name=value.getString("name");kind=value.getString("kind");
   status=value.optString("status","failed");uri=value.optString("uri");mime=value.optString("mime","application/octet-stream");
   error=value.optString("error");bytes=value.optLong("bytes");total=value.optLong("total",-1);retryAt=value.optLong("retryAt");
  }
  Job() {}
 }

 public static synchronized AppDownloads get(Context context) {
  if(instance==null)instance=new AppDownloads(context.getApplicationContext());
  return instance;
 }
 private AppDownloads(Context app) {
  context=app;ledger=new AtomicFile(new File(context.getFilesDir(),"downloads.json"));
  NotificationManager manager=context.getSystemService(NotificationManager.class);
  manager.createNotificationChannel(new NotificationChannel("downloads","文件下载",NotificationManager.IMPORTANCE_LOW));
  try {
   JSONArray saved=new JSONArray(new String(ledger.readFully(),"UTF-8"));
   for(int i=0;i<saved.length();i++){Job job=new Job(saved.getJSONObject(i));jobs.add(job);nextId=Math.max(nextId,job.id+1);}
   for(Job job:jobs)if(active(job)) {
    // A durable finishing record means the stream was closed and length checked.
    if("finishing".equals(job.status)&&!job.uri.isEmpty()) {
     ContentValues values=new ContentValues();values.put(MediaStore.Downloads.IS_PENDING,0);
     if(context.getContentResolver().update(Uri.parse(job.uri),values,null,null)>0){job.status="complete";continue;}
    }
    discardPending(job);job.status="failed";job.error="下载中断，点击重试";job.retryAt=System.currentTimeMillis()+60000;
   }
   persist();
  } catch(FileNotFoundException missing) {
   // No ledger on first launch; legacy completion records are migrated below.
  } catch(Exception e) {storageError="无法读取下载记录："+e.getMessage();}
 }
 private static boolean active(Job job){return "queued".equals(job.status)||"downloading".equals(job.status)||"finishing".equals(job.status);}
 public synchronized boolean isActive(){for(Job job:jobs)if(active(job))return true;return false;}
 private synchronized void persist() throws IOException {
  FileOutputStream output=null;
  try {JSONArray data=new JSONArray();for(Job job:jobs)data.put(job.json());output=ledger.startWrite();output.write(data.toString().getBytes("UTF-8"));ledger.finishWrite(output);}
  catch(Exception e){if(output!=null)ledger.failWrite(output);throw new IOException("下载记录保存失败",e);}
 }
 public synchronized String snapshot() {
  try {JSONArray data=new JSONArray();for(int i=jobs.size()-1;i>=0;i--)data.put(jobs.get(i).json());return new JSONObject().put("items",data).put("error",storageError).toString();}
  catch(JSONException e){return "{\"items\":[],\"error\":\"无法读取下载列表\"}";}
 }
 private String category(String kind){return "image".equals(kind)?"图片":"video".equals(kind)?"视频":"文件";}
 private String safeName(String name){return name.replaceAll("[\\\\/\\p{Cntrl}]","_");}
 private Job find(String key){for(Job job:jobs)if(job.key.equals(key))return job;return null;}

 public synchronized void importLegacy(JSONArray peers) throws Exception {
  if(!storageError.isEmpty())return;
  SharedPreferences legacy=context.getSharedPreferences("auto-saved",Context.MODE_PRIVATE);
  for(int i=0;i<peers.length();i++) {
   JSONObject peer=peers.getJSONObject(i);JSONArray items=peer.getJSONArray("items");
   for(int j=0;j<items.length();j++) {
    JSONObject item=items.getJSONObject(j);String key=peer.getString("id")+"/"+item.getString("id");
    if("text".equals(item.getString("kind"))||find(key)!=null||legacy.getLong(key,0)!=-1)continue;
    Job job=new Job();job.id=nextId++;job.key=key;job.name=safeName(item.getString("name"));job.kind=item.getString("kind");
    job.url=HOME+"/api/content/"+Uri.encode(peer.getString("id"))+"/"+Uri.encode(item.getString("id"))+"?download=1";
    job.total=item.getLong("size");job.bytes=job.total;job.uri=legacyFile(job.name,job.kind,job.total);
    job.status=job.uri.isEmpty()?"unavailable":"complete";
    if(!job.uri.isEmpty()){String mime=context.getContentResolver().getType(Uri.parse(job.uri));if(mime!=null)job.mime=mime;}
    jobs.add(job);try{persist();}catch(IOException e){jobs.remove(job);throw e;}
   }
  }
 }

 public synchronized int enqueue(String url,String name,String kind,long total) throws Exception {
  Uri source=Uri.parse(url);List<String> parts=source.getPathSegments();
  if(!"http".equals(source.getScheme())||!"127.0.0.1".equals(source.getHost())||source.getPort()!=48773||parts.size()!=4||!"api".equals(parts.get(0))||!"content".equals(parts.get(1)))throw new IOException("无效的下载地址");
  if(!storageError.isEmpty())throw new IOException(storageError);
  String key=parts.get(2)+"/"+parts.get(3);Job existing=find(key);
  if(existing!=null){if("failed".equals(existing.status))retry(existing.id);return existing.id;}
  Job job=new Job();job.id=nextId++;job.key=key;job.url=url;job.name=safeName(name);job.kind=kind;job.total=total;
  SharedPreferences legacy=context.getSharedPreferences("auto-saved",Context.MODE_PRIVATE);
  if(legacy.getLong(key,0)==-1){String old=legacyFile(job.name,kind,total);if(!old.isEmpty()){job.uri=old;job.status="complete";job.bytes=total;String mime=context.getContentResolver().getType(Uri.parse(old));if(mime!=null)job.mime=mime;}}
  jobs.add(job);
  try{persist();}catch(IOException e){jobs.remove(job);throw e;}
  if(!"complete".equals(job.status))worker.execute(()->transfer(job));
  return job.id;
 }
 public synchronized void retry(int id) throws IOException {
  for(Job job:jobs)if(job.id==id&&("failed".equals(job.status)||"unavailable".equals(job.status))) {
   if(!storageError.isEmpty())throw new IOException(storageError);
   job.status="queued";job.error="";job.bytes=0;
   try{persist();}catch(IOException e){job.status="failed";throw e;}
   worker.execute(()->transfer(job));return;
  }
 }
 public synchronized Intent openIntent(int id) throws IOException {
  for(Job job:jobs)if(job.id==id&&"complete".equals(job.status)&&!job.uri.isEmpty())return new Intent(Intent.ACTION_VIEW).setDataAndType(Uri.parse(job.uri),job.mime).addFlags(Intent.FLAG_GRANT_READ_URI_PERMISSION|Intent.FLAG_ACTIVITY_NEW_TASK);
  throw new IOException("文件尚未下载完成");
 }

 public synchronized void autoSave(JSONArray peers) throws Exception {
  if(!storageError.isEmpty())return;
  SharedPreferences legacy=context.getSharedPreferences("auto-saved",Context.MODE_PRIVATE);
  for(int i=0;i<peers.length();i++) {
   JSONObject peer=peers.getJSONObject(i);if(!peer.optBoolean("online")||!peer.optBoolean("enabled")||peer.optBoolean("locked"))continue;
   JSONArray items=peer.getJSONArray("items");
   for(int j=0;j<items.length();j++) {
    JSONObject item=items.getJSONObject(j);String kind=item.getString("kind");if("text".equals(kind))continue;
    String key=peer.getString("id")+"/"+item.getString("id");Job existing=find(key);
    if(existing!=null){if("failed".equals(existing.status)&&System.currentTimeMillis()>=existing.retryAt)retry(existing.id);continue;}
    long old=legacy.getLong(key,0);if(old==-1)continue;
    if(old>0&&retainLegacy(legacy,key,old,item))continue;
    enqueue(HOME+"/api/content/"+Uri.encode(peer.getString("id"))+"/"+Uri.encode(item.getString("id"))+"?download=1",item.getString("name"),kind,item.getLong("size"));
   }
  }
 }
 private boolean retainLegacy(SharedPreferences legacy,String key,long id,JSONObject item) throws Exception {
  // Read old system jobs only; all new transfers use our own HTTP worker.
  DownloadManager old=context.getSystemService(DownloadManager.class);
  try(Cursor cursor=old.query(new DownloadManager.Query().setFilterById(id))) {
   if(cursor!=null&&cursor.moveToFirst()) {
    int status=cursor.getInt(cursor.getColumnIndexOrThrow(DownloadManager.COLUMN_STATUS));
    if(status==DownloadManager.STATUS_SUCCESSFUL){if(!legacy.edit().putLong(key,-1).commit())throw new IOException("无法迁移已保存记录");return true;}
    if(status!=DownloadManager.STATUS_FAILED)return true;
   }
  }
  // Older versions persisted only a system job ID. If that history was removed,
  // recover the old timestamp-named output for this previously queued item.
  if(!legacyFile(safeName(item.getString("name")),item.getString("kind"),item.getLong("size")).isEmpty()) {
   if(!legacy.edit().putLong(key,-1).commit())throw new IOException("无法迁移已保存记录");return true;
  }
  return false;
 }
 private String legacyFile(String name,String kind,long size) {
  if(size<0)return "";
  String path=Environment.DIRECTORY_DOWNLOADS+"/邻享/"+category(kind)+"/";
  try(Cursor rows=context.getContentResolver().query(MediaStore.Downloads.EXTERNAL_CONTENT_URI,new String[]{"_id","_display_name"},"relative_path=? AND _size=? AND is_pending=0",new String[]{path,Long.toString(size)},null)) {
   while(rows!=null&&rows.moveToNext()){String stored=rows.getString(1),suffix="-"+name;if(stored.endsWith(suffix)&&stored.substring(0,stored.length()-suffix.length()).matches("[0-9]+"))return ContentUris.withAppendedId(MediaStore.Downloads.EXTERNAL_CONTENT_URI,rows.getLong(0)).toString();}
  }catch(Exception e){android.util.Log.w("LinShare","Legacy download lookup failed",e);}
  return "";
 }
 private void discardPending(Job job) {
  if(job.uri.isEmpty())return;
  try {context.getContentResolver().delete(Uri.parse(job.uri),"is_pending=1",null);}catch(Exception e){android.util.Log.w("LinShare","Partial download cleanup failed",e);}
  job.uri="";
 }
 private void transfer(Job job) {
  HttpURLConnection connection=null;
  try {
   synchronized(this){job.status="downloading";job.bytes=0;persist();notifyJob(job);}
   connection=(HttpURLConnection)new URL(job.url).openConnection();connection.setConnectTimeout(15000);connection.setReadTimeout(30000);connection.setInstanceFollowRedirects(false);
   if(connection.getResponseCode()!=200)throw new IOException("下载失败（HTTP "+connection.getResponseCode()+"）");
   synchronized(this) {
    long length=connection.getContentLengthLong();if(job.total<0)job.total=length;
    String mime=connection.getContentType();if(mime!=null)job.mime=mime.split(";",2)[0];
    if(job.mime.startsWith("image/"))job.kind="image";else if(job.mime.startsWith("video/"))job.kind="video";
    ContentValues values=new ContentValues();values.put(MediaStore.Downloads.DISPLAY_NAME,job.name);values.put(MediaStore.Downloads.MIME_TYPE,job.mime);
    values.put(MediaStore.Downloads.RELATIVE_PATH,Environment.DIRECTORY_DOWNLOADS+"/邻享/"+category(job.kind));values.put(MediaStore.Downloads.IS_PENDING,1);
    Uri output=context.getContentResolver().insert(MediaStore.Downloads.EXTERNAL_CONTENT_URI,values);if(output==null)throw new IOException("无法创建下载文件");
    job.uri=output.toString();persist();
   }
   long count=0,lastNotification=0;
   try(InputStream input=connection.getInputStream();OutputStream output=context.getContentResolver().openOutputStream(Uri.parse(job.uri),"w")) {
    if(output==null)throw new IOException("无法写入下载文件");
    byte[] buffer=new byte[65536];int n;
    while((n=input.read(buffer))!=-1){output.write(buffer,0,n);count+=n;synchronized(this){job.bytes=count;if(System.currentTimeMillis()-lastNotification>=1000){notifyJob(job);lastNotification=System.currentTimeMillis();}}}
   }
   synchronized(this) {
    if(job.total>=0&&count!=job.total)throw new IOException("文件未传完，点击重试");
    job.bytes=count;job.total=count;job.status="finishing";persist();
    ContentValues ready=new ContentValues();ready.put(MediaStore.Downloads.IS_PENDING,0);
    if(context.getContentResolver().update(Uri.parse(job.uri),ready,null,null)!=1)throw new IOException("无法完成文件保存");
    job.status="complete";job.error="";persist();notifyJob(job);
   }
  } catch(Exception e) {
   synchronized(this) {
    if(!"complete".equals(job.status)){discardPending(job);job.status="failed";job.error=e.getMessage()==null?"下载失败":e.getMessage();job.retryAt=System.currentTimeMillis()+60000;}
    try{persist();}catch(IOException failure){storageError=failure.getMessage();}
    notifyJob(job);
   }
  } finally {if(connection!=null)connection.disconnect();}
 }
 private void notifyJob(Job job) {
  try {
   Intent action=new Intent(context,MainActivity.class).putExtra("showDownloads",true).addFlags(Intent.FLAG_ACTIVITY_CLEAR_TOP|Intent.FLAG_ACTIVITY_SINGLE_TOP);
   if("complete".equals(job.status))action=openIntent(job.id);
   PendingIntent open=PendingIntent.getActivity(context,job.id,action,PendingIntent.FLAG_UPDATE_CURRENT|PendingIntent.FLAG_IMMUTABLE);
   Notification.Builder notice=new Notification.Builder(context,"downloads").setSmallIcon(context.getResources().getIdentifier("notification_icon","drawable",context.getPackageName()))
    .setContentTitle(job.name).setContentText("complete".equals(job.status)?"下载完成 · 点击打开":"failed".equals(job.status)?"下载失败 · 点击重试":"正在下载")
    .setContentIntent(open).setOnlyAlertOnce(true).setAutoCancel(!active(job)).setOngoing(active(job));
   if(active(job))notice.setProgress(100,job.total>0?(int)(job.bytes*100/job.total):0,job.total<=0);
   context.getSystemService(NotificationManager.class).notify(job.id,notice.build());
  }catch(Exception e){android.util.Log.w("LinShare","Download notification unavailable",e);}
 }
}
