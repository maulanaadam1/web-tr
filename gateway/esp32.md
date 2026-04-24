// =====================================================
// ESP32 FRP-Lite Gateway — WebSocket Bridge Edition
// =====================================================
// Library yang dibutuhkan (semua built-in di ESP32 Arduino core):
//   - WiFi.h, WebServer.h, HTTPClient.h, Preferences.h, ESPmDNS.h
//   - TIDAK perlu library tambahan!
//
// Cara kerja:
//   ESP32 → HTTP Upgrade → VPS /api/bridge/{name}
//   VPS auto-assign port internal → go2rtc konek otomatis
//   Tidak perlu setting port manual di EasyPanel/VPS.
//
// Rename file ini menjadi esp32.ino lalu upload via Arduino IDE.
// =====================================================

#include <ESPmDNS.h>
#include <HTTPClient.h>
#include <Preferences.h>
#include <WebServer.h>
#include <WiFi.h>
#include <WiFiClient.h>

WebServer server(80);
Preferences pref;

// FIX #1: Mutex untuk thread-safe akses status
SemaphoreHandle_t statusMutex;

// =====================================================
// Konfigurasi Global
// =====================================================
struct Config {
  char wifi_ssid[32];
  char wifi_pass[32];
  char vps_url[128];  // e.g. https://rtsp2go.campod.my.id
  char api_user[32];
  char api_pass[32];
} settings;

// =====================================================
// Konfigurasi Per Kamera (tanpa vps_port!)
// =====================================================
#define MAX_CAMS 4
struct CCTV {
  char name[48];         // slug unik, e.g. "gedung-a-lobby"
  char display_name[48]; // nama tampilan
  char local_ip[32];     // IP kamera lokal
  int  local_port;       // port RTSP kamera (default 554)
} cams[MAX_CAMS];

// =====================================================
// Status Runtime per Kamera
// =====================================================
struct RuntimeStatus {
  String bridge_status = "Idle";
  String last_error    = "None";
  bool   is_active     = false;
} camStatus[MAX_CAMS];

TaskHandle_t tunnelTasks[MAX_CAMS];
String global_vps_ping        = "Checking...";
unsigned long last_ping_time  = 0;
TaskHandle_t pingTaskHandle   = NULL;

// =====================================================
// Helper: JSON Escape
// =====================================================
String escapeJson(const String& s) {
  String out;
  out.reserve(s.length() + 8);
  for (unsigned int i = 0; i < s.length(); i++) {
    char c = s[i];
    if      (c == '"')  out += "\\\"";
    else if (c == '\\') out += "\\\\";
    else if (c == '\n') out += "\\n";
    else if (c == '\r') out += "\\r";
    else                out += c;
  }
  return out;
}

// Thread-safe status update
void setStatus(int idx, bool active, const String& bridge, const String& err) {
  if (xSemaphoreTake(statusMutex, pdMS_TO_TICKS(100)) == pdTRUE) {
    camStatus[idx].is_active     = active;
    camStatus[idx].bridge_status = bridge;
    if (err.length() > 0) camStatus[idx].last_error = err;
    xSemaphoreGive(statusMutex);
  }
}

// =====================================================
// UI HTML
// =====================================================
String getHTML() {
  bool isTrial = (strlen(settings.api_user) == 0);

  String html = "<!DOCTYPE html><html><head>";
  html += "<meta charset='UTF-8'><meta name='viewport' content='width=device-width,initial-scale=1'>";
  html += "<style>";
  html += "body{font-family:sans-serif;background:#f4f4f9;padding:16px;color:#333;margin:0}";
  html += ".wrap{max-width:620px;margin:auto}";
  html += "h1{font-size:1.3em;color:#2c3e50}h2{font-size:1em;color:#2c3e50;margin:0 0 8px}";
  html += "input{width:100%;padding:9px;margin:5px 0 10px;border:1px solid #ccc;border-radius:4px;box-sizing:border-box;font-size:14px}";
  html += ".card{background:#fff;padding:16px;border-radius:10px;box-shadow:0 2px 8px rgba(0,0,0,.08);margin-bottom:14px}";
  html += "button{background:#27ae60;color:#fff;padding:12px;border:none;width:100%;border-radius:6px;cursor:pointer;font-size:15px;font-weight:bold}";
  html += ".btn-vps{background:#2980b9;margin-top:8px}";
  html += "table{width:100%;border-collapse:collapse;font-size:13px}";
  html += "th,td{padding:8px;border-bottom:1px solid #eee;text-align:left}";
  html += ".on{color:#27ae60;font-weight:bold}.off{color:#e74c3c;font-weight:bold}.wait{color:#e67e22;font-weight:bold}";
  html += ".hdr{background:#eaf4fd;padding:10px;border-radius:6px;margin-bottom:14px;font-size:13px;text-align:center;color:#2980b9}";
  html += ".tag{font-size:10px;background:#e0e0e0;border-radius:3px;padding:1px 5px;margin-left:4px;color:#555}";
  html += ".tag-trial{background:#fdecea;color:#c0392b}";
  html += "</style></head><body><div class='wrap'>";
  html += "<h1>&#128249; ESP32 Bridge Gateway</h1>";

  html += "<div class='hdr'>Access: <b>http://rtsp2go.local</b> &nbsp;|&nbsp; ";
  html += "VPS: <span id='vps-ping'>" + global_vps_ping + "</span></div>";

  // Status table
  html += "<div class='card'><h2>&#128200; Status Kamera</h2>";
  html += "<table><thead><tr><th>Kamera</th><th>Bridge</th><th>Error</th></tr></thead>";
  html += "<tbody id='status-body'><tr><td colspan='3'>Memuat...</td></tr></tbody></table>";
  if (strlen(settings.vps_url) > 5) {
    html += "<button class='btn-vps' onclick=\"window.open('" + String(settings.vps_url) + "','_blank')\">&#127760; Buka Dashboard VPS</button>";
  }
  html += "</div>";

  // Global settings form
  html += "<form action='/save' method='POST'>";
  html += "<div class='card'><h2>&#9881; Konfigurasi Global</h2>";
  html += "WiFi SSID: <input name='ssid' value='" + String(settings.wifi_ssid) + "'>";
  html += "WiFi Password: <input name='pass' type='password' value='" + String(settings.wifi_pass) + "'>";
  html += "VPS URL: <input name='vps' placeholder='https://rtsp2go.campod.my.id' value='" + String(settings.vps_url) + "'>";
  html += "API User: <input name='a_user' placeholder='Kosongkan = Trial' value='" + String(settings.api_user) + "'>";
  html += "API Password: <input name='a_pass' type='password' placeholder='Kosongkan = Trial' value='" + String(settings.api_pass) + "'>";
  html += "</div>";

  // Per camera — NO vps_port field!
  for (int i = 0; i < MAX_CAMS; i++) {
    bool disabled = (isTrial && i >= 2);
    String dis = disabled ? " disabled" : "";
    html += "<div class='card' style='" + String(disabled ? "opacity:.5" : "") + "'>";
    html += "<h2>&#128247; Kamera " + String(i + 1);
    if (disabled) html += " <span class='tag tag-trial'>Trial: maks 2 kamera</span>";
    html += "</h2>";
    html += "Slug (ID Unik): <input name='n"    + String(i) + "' placeholder='gedung-a-lobby' value='" + String(cams[i].name)         + "'" + dis + ">";
    html += "Nama Tampilan: <input name='dn"    + String(i) + "' placeholder='Lobby Gedung A'  value='" + String(cams[i].display_name) + "'" + dis + ">";
    html += "IP Kamera Lokal: <input name='ip"  + String(i) + "' placeholder='192.168.1.100'   value='" + String(cams[i].local_ip)     + "'" + dis + ">";
    html += "Port RTSP (default 554): <input name='lp" + String(i) + "' type='number' placeholder='554' value='" +
            String(cams[i].local_port > 0 ? cams[i].local_port : 554) + "'" + dis + ">";
    html += "</div>";
  }
  html += "<button type='submit'>&#128190; Simpan &amp; Terapkan</button></form></div>";

  // Auto-refresh status
  html += "<script>";
  html += "function loadStatus(){";
  html += "  fetch('/status').then(r=>r.json()).then(d=>{";
  html += "    document.getElementById('vps-ping').innerText=d.vps_ping;";
  html += "    let rows='';";
  html += "    d.cams.forEach(c=>{";
  html += "      if(c.name.length>0){";
  html += "        let cls=c.active?'on':(c.bridge.includes('Idle')?'wait':'off');";
  html += "        rows+=`<tr><td><b>${c.display}</b><br><small style='color:#999'>${c.name}</small></td>`;";
  html += "        rows+=`<td class='${cls}'>${c.bridge}</td><td style='font-size:11px;color:#999'>${c.err}</td></tr>`;";
  html += "      }";
  html += "    });";
  html += "    document.getElementById('status-body').innerHTML=rows||'<tr><td colspan=\"3\">Tidak ada kamera aktif.</td></tr>';";
  html += "  }).catch(e=>console.error(e));";
  html += "}";
  html += "setInterval(loadStatus,4000);loadStatus();";
  html += "</script></body></html>";
  return html;
}

// =====================================================
// Persistent Storage
// =====================================================
void saveSettings() {
  pref.begin("esp32gw", false);
  pref.putBytes("cfg",  &settings, sizeof(Config));
  pref.putBytes("cams", &cams,     sizeof(cams));
  pref.end();
}

void loadSettings() {
  pref.begin("esp32gw", true);
  if (pref.isKey("cfg")) {
    pref.getBytes("cfg",  &settings, sizeof(Config));
    pref.getBytes("cams", &cams,     sizeof(cams));
  }
  pref.end();
}

// =====================================================
// Background Ping VPS (non-blocking)
// =====================================================
void pingVPSTask(void* pv) {
  while (true) {
    ulTaskNotifyTake(pdTRUE, portMAX_DELAY);
    if (WiFi.status() != WL_CONNECTED || strlen(settings.vps_url) < 5) {
      global_vps_ping = "WiFi Terputus";
      continue;
    }
    HTTPClient http;
    http.begin(String(settings.vps_url) + "/");
    http.setTimeout(3000);
    int code = http.GET();
    global_vps_ping = (code > 0)
      ? (String(code) + (code == 200 ? " OK" : " Connected"))
      : "Tidak Terjangkau";
    http.end();
  }
}

void triggerPing() {
  if (pingTaskHandle) xTaskNotifyGive(pingTaskHandle);
}

// =====================================================
// CORE: Tunnel Task — 1 task per kamera
// Menggunakan HTTP Upgrade (tanpa library eksternal!)
// Tidak perlu setting port VPS secara manual.
// =====================================================
void tunnelTask(void* pvParameters) {
  int idx = *(int*)pvParameters;
  delete (int*)pvParameters;

  // Parse VPS host dan port dari URL
  String baseURL = String(settings.vps_url);
  bool   useSSL  = baseURL.startsWith("https://");
  String host    = baseURL;
  host.replace("https://", "");
  host.replace("http://", "");
  int colonPos = host.indexOf(':');
  int port     = useSSL ? 443 : 80;
  if (colonPos != -1) {
    port = host.substring(colonPos + 1).toInt();
    host = host.substring(0, colonPos);
  }
  int slashPos = host.indexOf('/');
  if (slashPos != -1) host = host.substring(0, slashPos);
  host.trim();

  const int BUF_SIZE = 1024;
  uint8_t   buf[BUF_SIZE];

  while (true) {
    if (WiFi.status() != WL_CONNECTED) {
      setStatus(idx, false, "Offline - WiFi", "WiFi tidak terhubung");
      vTaskDelay(pdMS_TO_TICKS(5000));
      continue;
    }

    // Baca konfigurasi terkini
    String camName    = String(cams[idx].name);
    String dispName   = String(cams[idx].display_name);
    String localIP    = String(cams[idx].local_ip);
    int    localPort  = cams[idx].local_port > 0 ? cams[idx].local_port : 554;

    if (camName.length() < 2) {
      setStatus(idx, false, "Idle - Belum Dikonfigurasi", "Isi Slug kamera");
      vTaskDelay(pdMS_TO_TICKS(10000));
      continue;
    }

    // === Koneksi ke kamera lokal ===
    WiFiClient localClient;
    if (!localClient.connect(localIP.c_str(), localPort)) {
      setStatus(idx, false, "Offline - Kamera Lokal", "Kamera " + localIP + ":" + String(localPort) + " tidak merespons");
      vTaskDelay(pdMS_TO_TICKS(8000));
      continue;
    }

    // === Koneksi ke VPS via HTTP Upgrade ===
    WiFiClient vpsClient;
    // Untuk HTTPS, gunakan WiFiClientSecure dengan setInsecure()
    // Ini sengaja tidak pakai TLS penuh agar tidak butuh sertifikat CA
    if (!vpsClient.connect(host.c_str(), port)) {
      localClient.stop();
      setStatus(idx, false, "Offline - VPS Unreachable", "Gagal konek ke " + host);
      vTaskDelay(pdMS_TO_TICKS(8000));
      continue;
    }

    // Kirim HTTP Upgrade request
    String authHeader = "";
    if (strlen(settings.api_user) > 0) {
      // Basic Auth manual (tidak perlu library)
      // Format: "user:pass" dalam Base64 — untuk kesederhanaan pakai query param
      authHeader = "Authorization: Basic "; // TODO: encode base64 jika diperlukan
    }

    String req = "GET /api/bridge/" + camName + " HTTP/1.1\r\n";
    req += "Host: " + host + "\r\n";
    req += "Upgrade: rtsp-bridge\r\n";
    req += "Connection: Upgrade\r\n";
    req += "X-Display-Name: " + dispName + "\r\n";
    if (strlen(settings.api_user) > 0) {
      req += "X-Api-User: " + String(settings.api_user) + "\r\n";
      req += "X-Api-Pass: " + String(settings.api_pass) + "\r\n";
    }
    req += "\r\n";
    vpsClient.print(req);

    // Tunggu dan validasi respons 101
    unsigned long t = millis();
    String responseHeader = "";
    while (vpsClient.connected() && millis() - t < 5000) {
      if (vpsClient.available()) {
        char c = vpsClient.read();
        responseHeader += c;
        if (responseHeader.endsWith("\r\n\r\n")) break;
      }
      vTaskDelay(1);
    }

    if (!responseHeader.startsWith("HTTP/1.1 101")) {
      vpsClient.stop();
      localClient.stop();
      setStatus(idx, false, "Error - VPS Rejected", "Respons: " + responseHeader.substring(0, 40));
      vTaskDelay(pdMS_TO_TICKS(8000));
      continue;
    }

    // === Bridge Aktif: relay data antara VPS ↔ Kamera Lokal ===
    setStatus(idx, true, "Active ⚡", "None");
    Serial.printf("[Bridge #%d] %s - Tunnel aktif!\n", idx, camName.c_str());

    while (vpsClient.connected() && localClient.connected()) {
      // VPS → Kamera Lokal
      int avail = vpsClient.available();
      if (avail > 0) {
        int n = vpsClient.read(buf, min(avail, BUF_SIZE));
        if (n > 0) localClient.write(buf, n);
      }
      // Kamera Lokal → VPS
      avail = localClient.available();
      if (avail > 0) {
        int n = localClient.read(buf, min(avail, BUF_SIZE));
        if (n > 0) vpsClient.write(buf, n);
      }
      vTaskDelay(1);
    }

    vpsClient.stop();
    localClient.stop();
    setStatus(idx, false, "Offline - Terputus", "Koneksi terputus, mencoba ulang...");
    Serial.printf("[Bridge #%d] %s - Terputus, reconnect dalam 5s\n", idx, camName.c_str());
    vTaskDelay(pdMS_TO_TICKS(5000));
  }
}

// =====================================================
// Manajemen Task Kamera
// =====================================================
void refreshCameraTasks() {
  bool isTrial = (strlen(settings.api_user) == 0);
  for (int i = 0; i < MAX_CAMS; i++) {
    if (tunnelTasks[i] != NULL) {
      vTaskDelete(tunnelTasks[i]);
      tunnelTasks[i] = NULL;
    }
    if (isTrial && i >= 2) {
      setStatus(i, false, "Disabled (Trial)", "Upgrade akun untuk unlock");
      continue;
    }
    if (strlen(cams[i].local_ip) > 6 && strlen(cams[i].name) > 1) {
      int* pIdx = new int(i);
      xTaskCreate(tunnelTask, ("Tun" + String(i)).c_str(), 8192, pIdx, 1, &tunnelTasks[i]);
    }
  }
}

// =====================================================
// Setup
// =====================================================
void setup() {
  Serial.begin(115200);

  // Inisialisasi mutex sebelum task apapun
  statusMutex = xSemaphoreCreateMutex();

  loadSettings();

  WiFi.begin(settings.wifi_ssid, settings.wifi_pass);
  int retry = 0;
  while (WiFi.status() != WL_CONNECTED && retry < 24) {
    delay(500); retry++;
    Serial.print(".");
  }
  Serial.println();

  // FIX: mDNS hanya jika WiFi terhubung
  if (WiFi.status() == WL_CONNECTED) {
    MDNS.begin("rtsp2go");
    Serial.println("WiFi: " + WiFi.localIP().toString());
    Serial.println("mDNS: http://rtsp2go.local");
  } else {
    WiFi.softAP("ESP32-Gateway", "12345678");
    Serial.println("AP Mode: ESP32-Gateway | 192.168.4.1");
  }

  // Background ping task
  xTaskCreate(pingVPSTask, "Ping", 4096, NULL, 1, &pingTaskHandle);

  // =====================================================
  // Web Server Routes
  // =====================================================
  server.on("/", []() {
    server.send(200, "text/html", getHTML());
  });

  server.on("/status", []() {
    String json = "{\"vps_ping\":\"" + escapeJson(global_vps_ping) + "\",\"cams\":[";
    if (xSemaphoreTake(statusMutex, pdMS_TO_TICKS(200)) == pdTRUE) {
      for (int i = 0; i < MAX_CAMS; i++) {
        json += "{\"name\":\""    + escapeJson(String(cams[i].name)) +
                "\",\"display\":\"" + escapeJson(String(cams[i].display_name)) +
                "\",\"bridge\":\"" + escapeJson(camStatus[i].bridge_status) +
                "\",\"err\":\""  + escapeJson(camStatus[i].last_error) +
                "\",\"active\":" + String(camStatus[i].is_active ? "true" : "false") + "}";
        if (i < MAX_CAMS - 1) json += ",";
      }
      xSemaphoreGive(statusMutex);
    }
    json += "]}";
    server.send(200, "application/json", json);
  });

  server.on("/save", HTTP_POST, []() {
    bool needsRestart = (String(settings.wifi_ssid) != server.arg("ssid") ||
                         String(settings.vps_url)   != server.arg("vps"));

    snprintf(settings.wifi_ssid, 32,  "%s", server.arg("ssid").c_str());
    snprintf(settings.wifi_pass, 32,  "%s", server.arg("pass").c_str());
    snprintf(settings.vps_url,   128, "%s", server.arg("vps").c_str());
    snprintf(settings.api_user,  32,  "%s", server.arg("a_user").c_str());
    snprintf(settings.api_pass,  32,  "%s", server.arg("a_pass").c_str());

    for (int i = 0; i < MAX_CAMS; i++) {
      snprintf(cams[i].name,         48, "%s", server.arg("n"  + String(i)).c_str());
      snprintf(cams[i].display_name, 48, "%s", server.arg("dn" + String(i)).c_str());
      snprintf(cams[i].local_ip,     32, "%s", server.arg("ip" + String(i)).c_str());
      cams[i].local_port = server.arg("lp" + String(i)).toInt();
    }
    saveSettings();

    if (needsRestart) {
      server.send(200, "text/html", "<h2>&#128257; Network Berubah. Restart dalam 2 detik...</h2>");
      delay(2000); ESP.restart();
    } else {
      refreshCameraTasks();
      server.send(200, "text/html",
        "<html><body onload='setTimeout(()=>{location.href=\"/\"},2000)'>"
        "<h2>&#10003; Konfigurasi Tersimpan!</h2><p>Mengalihkan...</p></body></html>");
    }
  });

  server.begin();

  if (WiFi.status() == WL_CONNECTED) {
    refreshCameraTasks();
    triggerPing();
  }
}

// =====================================================
// Loop — minimal, semua kerja di FreeRTOS tasks
// =====================================================
void loop() {
  server.handleClient();
  if (millis() - last_ping_time > 60000) {
    triggerPing();
    last_ping_time = millis();
  }
  delay(10);
}