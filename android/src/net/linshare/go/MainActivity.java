package net.linshare.go;

import android.app.*;
import android.content.*;
import android.graphics.Color;
import android.net.Uri;
import android.os.*;
import android.view.*;
import android.webkit.*;
import android.widget.*;
import java.net.*;
import java.util.*;

public class MainActivity extends Activity {
 private static final String HOME="http://127.0.0.1:48773";
 private WebView web;
 private ValueCallback<Uri[]> chooser;
 private final Handler handler=new Handler(Looper.getMainLooper());
 private volatile boolean destroyed;
 private int statusInset;
 @Override public void onCreate(Bundle saved) {
  super.onCreate(saved);
  getWindow().setStatusBarColor(Color.TRANSPARENT);getWindow().setStatusBarContrastEnforced(false);getWindow().setNavigationBarColor(Color.rgb(17,21,27));
  if(Build.VERSION.SDK_INT>=30)getWindow().setDecorFitsSystemWindows(false);else getWindow().getDecorView().setSystemUiVisibility(View.SYSTEM_UI_FLAG_LAYOUT_STABLE|View.SYSTEM_UI_FLAG_LAYOUT_FULLSCREEN);
  startForegroundService(new Intent(this,ShareService.class));
  LinearLayout root=new LinearLayout(this);root.setOrientation(LinearLayout.VERTICAL);root.setBackgroundColor(Color.rgb(17,19,32));
  root.setOnApplyWindowInsetsListener((v,insets)->{android.graphics.Insets bars=insets.getInsets(WindowInsets.Type.systemBars()|WindowInsets.Type.ime());statusInset=Math.round(bars.top/getResources().getDisplayMetrics().density);v.setPadding(bars.left,0,bars.right,bars.bottom);applyStatusInset();return insets;});
  TextView loading=new TextView(this);loading.setText("邻享\n正在启动附近共享……");loading.setTextSize(20);loading.setTextColor(Color.rgb(213,245,138));loading.setGravity(Gravity.CENTER);root.addView(loading,new LinearLayout.LayoutParams(-1,-1));
  web=new WebView(this);web.setVisibility(View.GONE);web.setBackgroundColor(Color.rgb(17,21,27));root.addView(web,new LinearLayout.LayoutParams(-1,-1));setContentView(root);
  WebSettings settings=web.getSettings();settings.setJavaScriptEnabled(true);settings.setDomStorageEnabled(true);settings.setAllowFileAccess(false);settings.setAllowContentAccess(true);settings.setMediaPlaybackRequiresUserGesture(true);settings.setSupportMultipleWindows(false);
  web.addJavascriptInterface(new Bridge(),"Android");
  web.setWebViewClient(new WebViewClient(){@Override public void onPageFinished(WebView view,String url){applyStatusInset();showRequestedDownloads();}
   @Override public boolean shouldOverrideUrlLoading(WebView view,WebResourceRequest request){return !isLocal(request.getUrl().toString());}});
  web.setWebChromeClient(new WebChromeClient(){@Override public boolean onShowFileChooser(WebView view,ValueCallback<Uri[]> callback,FileChooserParams params){
   if(chooser!=null)chooser.onReceiveValue(null);chooser=callback;
   Intent intent=new Intent(Intent.ACTION_OPEN_DOCUMENT);intent.addCategory(Intent.CATEGORY_OPENABLE);intent.setType("*/*");
   ArrayList<String> types=new ArrayList<>();for(String value:params.getAcceptTypes())for(String type:value.split(","))if(!type.trim().isEmpty())types.add(type.trim());
   if(!types.isEmpty())intent.putExtra(Intent.EXTRA_MIME_TYPES,types.toArray(new String[0]));
   intent.putExtra(Intent.EXTRA_ALLOW_MULTIPLE,true);intent.addFlags(Intent.FLAG_GRANT_READ_URI_PERMISSION|Intent.FLAG_GRANT_PERSISTABLE_URI_PERMISSION);
   try{startActivityForResult(intent,10);}catch(ActivityNotFoundException e){chooser.onReceiveValue(null);chooser=null;Toast.makeText(MainActivity.this,"没有可用的系统文件选择器",Toast.LENGTH_LONG).show();}return true;
  }});
  web.setDownloadListener((url,agent,disposition,mime,length)->new Bridge().download(url,URLUtil.guessFileName(url,disposition,mime),"file",length));
  if(Build.VERSION.SDK_INT>=33&&checkSelfPermission("android.permission.POST_NOTIFICATIONS")!=android.content.pm.PackageManager.PERMISSION_GRANTED)requestPermissions(new String[]{"android.permission.POST_NOTIFICATIONS"},20);
  new Thread(()->{for(int i=0;i<100&&!destroyed;i++){if(ShareService.startupError!=null){handler.post(()->loading.setText(ShareService.startupError));return;}try{HttpURLConnection c=(HttpURLConnection)new URL(HOME+"/api/state").openConnection();c.setConnectTimeout(300);c.setReadTimeout(300);int status=c.getResponseCode();c.disconnect();if(status==200){handler.post(()->{if(!destroyed){loading.setVisibility(View.GONE);web.setVisibility(View.VISIBLE);web.loadUrl(HOME);}});return;}}catch(Exception ignored){}try{Thread.sleep(200);}catch(InterruptedException e){return;}}
   handler.post(()->{if(!destroyed){TextView error=new TextView(this);error.setText("共享服务未能启动。请重新打开邻享。\n可在应用日志中查看原因。");error.setTextColor(Color.WHITE);error.setPadding(30,80,30,30);setContentView(error);}});
  }).start();
 }
 private void applyStatusInset(){if(web!=null)web.evaluateJavascript("document.documentElement.style.setProperty('--status-inset','"+statusInset+"px')",null);}
 private boolean isLocal(String url){Uri uri=Uri.parse(url);return "http".equals(uri.getScheme())&&"127.0.0.1".equals(uri.getHost())&&uri.getPort()==48773;}
 @Override protected void onActivityResult(int request,int result,Intent data){super.onActivityResult(request,result,data);if(request==10&&chooser!=null){ArrayList<Uri> uris=new ArrayList<>();if(result==RESULT_OK&&data!=null){if(data.getClipData()!=null){for(int i=0;i<data.getClipData().getItemCount();i++)uris.add(data.getClipData().getItemAt(i).getUri());}else if(data.getData()!=null)uris.add(data.getData());for(Uri uri:uris)try{getContentResolver().takePersistableUriPermission(uri,Intent.FLAG_GRANT_READ_URI_PERMISSION);}catch(SecurityException ignored){}}chooser.onReceiveValue(uris.isEmpty()?null:uris.toArray(new Uri[0]));chooser=null;}}
 public class Bridge {
  @JavascriptInterface public void appBackgroundSettings(){runOnUiThread(()->startActivity(new Intent(android.provider.Settings.ACTION_APPLICATION_DETAILS_SETTINGS,Uri.parse("package:"+getPackageName()))));}
  @JavascriptInterface public void backgroundSettings(){runOnUiThread(()->{PowerManager power=(PowerManager)getSystemService(POWER_SERVICE);Intent intent=power.isIgnoringBatteryOptimizations(getPackageName())?new Intent(android.provider.Settings.ACTION_IGNORE_BATTERY_OPTIMIZATION_SETTINGS):new Intent(android.provider.Settings.ACTION_REQUEST_IGNORE_BATTERY_OPTIMIZATIONS,Uri.parse("package:"+getPackageName()));try{startActivity(intent);}catch(ActivityNotFoundException e){startActivity(new Intent(android.provider.Settings.ACTION_APPLICATION_DETAILS_SETTINGS,Uri.parse("package:"+getPackageName())));}});}
  @JavascriptInterface public void copyText(String text){((ClipboardManager)getSystemService(CLIPBOARD_SERVICE)).setPrimaryClip(ClipData.newPlainText("邻享",text));}
  @JavascriptInterface public void download(String url,String name,String kind,long size){try{AppDownloads.get(MainActivity.this).enqueue(url,name,kind,size);}catch(Exception e){runOnUiThread(()->Toast.makeText(MainActivity.this,e.getMessage(),Toast.LENGTH_LONG).show());}}
  @JavascriptInterface public String downloadState(){return AppDownloads.get(MainActivity.this).snapshot();}
  @JavascriptInterface public void retryDownload(int id){try{AppDownloads.get(MainActivity.this).retry(id);}catch(Exception e){runOnUiThread(()->Toast.makeText(MainActivity.this,e.getMessage(),Toast.LENGTH_LONG).show());}}
  @JavascriptInterface public void openDownload(int id){runOnUiThread(()->{try{startActivity(AppDownloads.get(MainActivity.this).openIntent(id));}catch(Exception e){Toast.makeText(MainActivity.this,"无法打开文件，请在下载/邻享中查看",Toast.LENGTH_LONG).show();}});}
 }
 private void showRequestedDownloads(){if(getIntent().getBooleanExtra("showDownloads",false)){getIntent().removeExtra("showDownloads");web.evaluateJavascript("if(typeof showDownloads==='function')showDownloads()",null);}}
 @Override protected void onNewIntent(Intent intent){super.onNewIntent(intent);setIntent(intent);showRequestedDownloads();}
 @Override protected void onResume(){super.onResume();if(web!=null)web.evaluateJavascript("if(typeof load===\"function\")load()",null);}
 @Override public void onBackPressed(){web.evaluateJavascript("!!document.querySelector('dialog[open]')",value->{if("true".equals(value))web.evaluateJavascript("document.querySelector('dialog[open]').close()",null);else moveTaskToBack(true);});}
 @Override protected void onDestroy(){destroyed=true;if(chooser!=null)chooser.onReceiveValue(null);web.destroy();super.onDestroy();}
}
