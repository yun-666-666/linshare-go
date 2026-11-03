package net.linshare.go;

import android.app.*;
import android.content.*;
import android.database.Cursor;
import android.net.Uri;
import android.net.wifi.WifiManager;
import android.os.*;
import java.io.*;
import java.net.*;
import org.json.*;

public class ShareService extends Service {
 public static volatile String startupError;
 private java.lang.Process core;
 private WifiManager.MulticastLock multicast;
 private WifiManager.WifiLock wifiLock;
 private PowerManager.WakeLock wakeLock;
 private volatile boolean destroyed;
 private Boolean lastEnabled;
 private static final String HOME="http://127.0.0.1:48773";
 private Notification notification(boolean enabled) {
  PendingIntent open=PendingIntent.getActivity(this,0,new Intent(this,MainActivity.class),PendingIntent.FLAG_IMMUTABLE);
  PendingIntent toggle=PendingIntent.getService(this,1,new Intent(this,ShareService.class).setAction("toggle"),PendingIntent.FLAG_IMMUTABLE);
  return new Notification.Builder(this,"sharing").setSmallIcon(getResources().getIdentifier("notification_icon","drawable",getPackageName()))
   .setContentTitle(enabled?"邻享正在附近共享":"邻享共享已暂停").setContentText("点击打开共享空间 · 熄屏可继续运行").setContentIntent(open).setOngoing(true)
   .addAction(new Notification.Action.Builder(null,enabled?"关闭共享":"开启共享",toggle).build()).build();
 }
 @Override public void onCreate() {
  super.onCreate(); startupError=null;
  NotificationManager manager=getSystemService(NotificationManager.class);
  manager.createNotificationChannel(new NotificationChannel("sharing","附近共享",NotificationManager.IMPORTANCE_LOW));
  startForeground(1,notification(true));
  WifiManager wifi=(WifiManager)getApplicationContext().getSystemService(WIFI_SERVICE);
  multicast=wifi.createMulticastLock("linshare-discovery");multicast.setReferenceCounted(false);multicast.acquire();
  wifiLock=wifi.createWifiLock(WifiManager.WIFI_MODE_FULL_HIGH_PERF,"linshare-sharing");wifiLock.setReferenceCounted(false);
  wakeLock=((PowerManager)getSystemService(POWER_SERVICE)).newWakeLock(PowerManager.PARTIAL_WAKE_LOCK,"linshare:sharing");wakeLock.setReferenceCounted(false);
  try {
   File data=new File(getFilesDir(),"shelf");data.mkdirs();NetworkSnapshot.write(data);
   ProcessBuilder builder=new ProcessBuilder(getApplicationInfo().nativeLibraryDir+"/libcore.so","-data",data.getAbsolutePath(),"-name",Build.MODEL,"-no-open");
   builder.directory(getFilesDir());builder.environment().put("HOME",getFilesDir().getAbsolutePath());builder.environment().put("TMPDIR",getCacheDir().getAbsolutePath());
   builder.redirectErrorStream(true);builder.redirectOutput(ProcessBuilder.Redirect.appendTo(new File(getFilesDir(),"core.log")));core=builder.start();
   new Thread(()->{while(!destroyed&&core.isAlive()){
    try {NetworkSnapshot.write(data);JSONObject state=request("/api/state",null);boolean enabled=state.getJSONObject("self").getBoolean("enabled");boolean auto=state.getJSONObject("self").optBoolean("autoSave");
     synchronized(this){if(!destroyed){boolean active=enabled||auto;if(active){if(!wakeLock.isHeld())wakeLock.acquire();if(!wifiLock.isHeld())wifiLock.acquire();}else{if(wakeLock.isHeld())wakeLock.release();if(wifiLock.isHeld())wifiLock.release();}}}
     if(lastEnabled==null||lastEnabled!=enabled){manager.notify(1,notification(enabled));lastEnabled=enabled;}
     if(auto)autoSave(state.getJSONArray("peers"));
    }catch(Exception e){android.util.Log.w("LinShare","Service update failed",e);}
    try{Thread.sleep(3000);}catch(InterruptedException e){return;}
   }}).start();
   new Thread(()->{try{int code=core.waitFor();if(!destroyed)startupError="共享服务已退出（"+code+"），请重新打开应用。";}catch(InterruptedException ignored){}}).start();
  }catch(Exception e){startupError="共享服务启动失败："+e.getMessage();android.util.Log.e("LinShare","Go core failed",e);stopSelf();}
 }
 private JSONObject request(String path,JSONObject body)throws Exception {
  HttpURLConnection c=(HttpURLConnection)new URL(HOME+path).openConnection();c.setConnectTimeout(2000);c.setReadTimeout(5000);
  try{if(body!=null){c.setRequestMethod("POST");c.setDoOutput(true);c.setRequestProperty("Content-Type","application/json");try(OutputStream out=c.getOutputStream()){out.write(body.toString().getBytes("UTF-8"));}}
   try(InputStream in=c.getInputStream();ByteArrayOutputStream out=new ByteArrayOutputStream()){byte[] buffer=new byte[8192];int n;while((n=in.read(buffer))!=-1)out.write(buffer,0,n);return new JSONObject(out.toString("UTF-8"));}
  }finally{c.disconnect();}
 }
 private void autoSave(JSONArray peers)throws Exception {
  SharedPreferences saved=getSharedPreferences("auto-saved",MODE_PRIVATE);DownloadManager downloads=(DownloadManager)getSystemService(DOWNLOAD_SERVICE);
  for(int i=0;i<peers.length();i++){JSONObject peer=peers.getJSONObject(i);if(!peer.optBoolean("online")||!peer.optBoolean("enabled")||peer.optBoolean("locked"))continue;
   JSONArray items=peer.getJSONArray("items");for(int j=0;j<items.length();j++){JSONObject item=items.getJSONObject(j);String kind=item.getString("kind");if("text".equals(kind))continue;
    String key=peer.getString("id")+"/"+item.getString("id");long download=saved.getLong(key,0);if(download==-1)continue;
    if(download>0){try(Cursor cursor=downloads.query(new DownloadManager.Query().setFilterById(download))){if(cursor!=null&&cursor.moveToFirst()){
     int status=cursor.getInt(cursor.getColumnIndexOrThrow(DownloadManager.COLUMN_STATUS));if(status==DownloadManager.STATUS_SUCCESSFUL){saved.edit().putLong(key,-1).apply();continue;}if(status!=DownloadManager.STATUS_FAILED)continue;
    }} // Retain failed jobs in system downloads; retry later without treating them as saved.
     long retry=saved.getLong(key+"/retry",0);if(System.currentTimeMillis()<retry)continue;
    }
    String category="image".equals(kind)?"图片":"video".equals(kind)?"视频":"文件";
    String name=item.getString("name").replace('/','_').replace('\\','_');
    DownloadManager.Request task=new DownloadManager.Request(Uri.parse(HOME+"/api/content/"+Uri.encode(peer.getString("id"))+"/"+Uri.encode(item.getString("id"))+"?download=1"));
    task.setTitle(name);task.setDescription("邻享自动保存 · "+peer.getString("name"));task.setNotificationVisibility(DownloadManager.Request.VISIBILITY_VISIBLE_NOTIFY_COMPLETED);
    task.setDestinationInExternalPublicDir(Environment.DIRECTORY_DOWNLOADS,"邻享/"+category+"/"+System.currentTimeMillis()+"-"+name);
    long id=downloads.enqueue(task);saved.edit().putLong(key,id).putLong(key+"/retry",System.currentTimeMillis()+60000).apply();
   }
  }
 }
 @Override public int onStartCommand(Intent intent,int flags,int id){
  if(intent!=null&&("toggle".equals(intent.getAction())||"stop".equals(intent.getAction())))new Thread(()->{try{JSONObject self=request("/api/state",null).getJSONObject("self");self.put("enabled","stop".equals(intent.getAction())?false:!self.getBoolean("enabled"));request("/api/settings",self);}catch(Exception e){android.util.Log.e("LinShare","Toggle sharing failed",e);}}).start();
  return START_STICKY;
 }
 @Override public synchronized void onDestroy(){destroyed=true;if(core!=null)core.destroy();if(multicast!=null&&multicast.isHeld())multicast.release();if(wifiLock!=null&&wifiLock.isHeld())wifiLock.release();if(wakeLock!=null&&wakeLock.isHeld())wakeLock.release();super.onDestroy();}
 @Override public IBinder onBind(Intent intent){return null;}
}
