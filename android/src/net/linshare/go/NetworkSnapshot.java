package net.linshare.go;

import java.io.*;
import java.net.*;
import java.util.*;
import org.json.*;

// Android exposes interface addresses through its platform API; Go's netlink
// enumeration is restricted for app UIDs on current Android versions.
final class NetworkSnapshot {
 static void write(File directory) throws Exception {
  JSONArray result=new JSONArray();
  Enumeration<NetworkInterface> interfaces=NetworkInterface.getNetworkInterfaces();
  while(interfaces!=null&&interfaces.hasMoreElements()) {
   NetworkInterface iface=interfaces.nextElement();if(!iface.isUp()||iface.isLoopback())continue;
   JSONArray addresses=new JSONArray();
   for(InterfaceAddress address:iface.getInterfaceAddresses())if(address.getAddress() instanceof Inet4Address)addresses.put(address.getAddress().getHostAddress()+"/"+address.getNetworkPrefixLength());
   JSONObject entry=new JSONObject();entry.put("index",iface.getIndex());entry.put("name",iface.getName());entry.put("cidrs",addresses);result.put(entry);
  }
  File temp=new File(directory,"interfaces.tmp");try(FileOutputStream output=new FileOutputStream(temp)){output.write(result.toString().getBytes("UTF-8"));}
  if(!temp.renameTo(new File(directory,"interfaces.json")))throw new IOException("Unable to save interface list");
 }
}
