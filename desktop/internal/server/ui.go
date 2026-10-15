package server

import (
	"html/template"
	"log"
	"net"
	"net/http"
	"sort"
	"time"
)

// localOnly restricts sensitive UI actions and the pair code to this machine.
func (s *Server) localOnly(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		host, _, err := net.SplitHostPort(r.RemoteAddr)
		if err != nil {
			host = r.RemoteAddr
		}

		ip := net.ParseIP(host)
		if ip == nil || !ip.IsLoopback() {
			writeJSON(w, http.StatusForbidden, map[string]string{
				"error": "local access only",
			})
			return
		}

		next(w, r)
	}
}

// handleUI serves the simple local browser UI.
func (s *Server) handleUI(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" && r.URL.Path != "/voice" {
		http.NotFound(w, r)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	if err := uiTemplate.Execute(w, nil); err != nil {
		log.Printf("[server] failed to render UI: %v", err)
	}
}

// handleUIStatus returns local-only status data for the browser UI.
func (s *Server) handleUIStatus(w http.ResponseWriter, r *http.Request) {
	count, totalSize, err := s.db.GetSyncStats()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{
			"error": "failed to query stats",
		})
		return
	}

	type pairedDevice struct {
		DeviceID   string `json:"device_id"`
		DeviceName string `json:"device_name"`
		PairedAt   string `json:"paired_at"`
		Recent     bool   `json:"recent"`
		LastIP     string `json:"last_ip,omitempty"`
		LastSeen   string `json:"last_seen,omitempty"`
		LastSeenMs int64  `json:"last_seen_ms,omitempty"`
	}

	paired := s.cfg.GetPairedDevices()
	devices := make([]pairedDevice, 0, len(paired))

	now := time.Now()
	recentWindow := 10 * time.Minute

	for _, d := range paired {
		lastSeen, _ := time.Parse(time.RFC3339, d.LastSeen)
		lastSeenMs := int64(0)
		recent := false
		if !lastSeen.IsZero() {
			lastSeenMs = now.Sub(lastSeen).Milliseconds()
			recent = now.Sub(lastSeen) <= recentWindow
		}

		devices = append(devices, pairedDevice{
			DeviceID:   d.DeviceID,
			DeviceName: d.DeviceName,
			PairedAt:   d.PairedAt,
			Recent:     recent,
			LastIP:     d.LastIP,
			LastSeen:   d.LastSeen,
			LastSeenMs: lastSeenMs,
		})
	}

	sort.Slice(devices, func(i, j int) bool {
		if devices[i].Recent != devices[j].Recent {
			return devices[i].Recent
		}
		return devices[i].DeviceName < devices[j].DeviceName
	})

	progress := s.syncMgr.GetProgress()
	audioStatus := s.audioReceiver.Status()
	speechStatus := s.speechStatus()
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"device_name":    s.cfg.DeviceName,
		"http_port":      s.cfg.HTTPPort,
		"pair_code":      s.cfg.GetPairCode(),
		"target_folder":  s.cfg.GetTargetFolder(),
		"synced_count":   count,
		"synced_size":    totalSize,
		"paired_devices": devices,
		"progress": map[string]interface{}{
			"status":        progress.Status.String(),
			"current_file":  progress.CurrentFile,
			"files_total":   progress.FilesTotal,
			"files_done":    progress.FilesDone,
			"bytes_total":   progress.BytesTotal,
			"bytes_done":    progress.BytesDone,
			"error_message": progress.ErrorMessage,
		},
		"audio":  audioStatus,
		"speech": speechStatus,
	})
}

var uiTemplate = template.Must(template.New("ui").Parse(`<!doctype html>
<html lang="zh-CN">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>CrossHare</title>
  <style>
    :root {
      color-scheme: light;
      --ink: #18202a;
      --muted: #5b6472;
      --line: #d8dee8;
      --paper: #ffffff;
      --page: #f3f6fa;
      --green: #18794e;
      --red: #ba1a1a;
      --blue: #0b63ce;
    }
    * { box-sizing: border-box; }
    body {
      margin: 0;
      min-height: 100vh;
      background: var(--page);
      color: var(--ink);
      font-family: system-ui, -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif;
      line-height: 1.5;
    }
    main {
      width: min(960px, calc(100% - 32px));
      margin: 0 auto;
      padding: 32px 0;
    }
    header {
      display: flex;
      justify-content: space-between;
      gap: 16px;
      align-items: flex-start;
      margin-bottom: 24px;
    }
    h1 {
      margin: 0 0 6px;
      font-size: 32px;
      line-height: 1.1;
      letter-spacing: 0;
    }
    .muted { color: var(--muted); }
    .grid {
      display: grid;
      grid-template-columns: repeat(4, minmax(0, 1fr));
      gap: 12px;
      margin-bottom: 18px;
    }
    .panel {
      background: var(--paper);
      border: 1px solid var(--line);
      border-radius: 8px;
      padding: 16px;
    }
    .wide { grid-column: span 2; }
    .full { grid-column: 1 / -1; }
    .section-head {
      display: flex;
      justify-content: space-between;
      gap: 12px;
      align-items: baseline;
      margin-bottom: 12px;
    }
    .label {
      color: var(--muted);
      font-size: 13px;
      margin-bottom: 6px;
    }
    .value {
      font-size: 24px;
      font-weight: 700;
      overflow-wrap: anywhere;
    }
    .pair-code {
      font-family: ui-monospace, "SFMono-Regular", Consolas, monospace;
      font-size: 36px;
      letter-spacing: 0;
    }
    .pair-code.secondary { font-size: 28px; }
    button {
      appearance: none;
      border: 1px solid var(--blue);
      border-radius: 8px;
      background: var(--blue);
      color: white;
      cursor: pointer;
      font: inherit;
      font-weight: 700;
      min-height: 42px;
      padding: 0 16px;
    }
    button:disabled {
      cursor: wait;
      opacity: 0.7;
    }
    .status {
      display: inline-flex;
      align-items: center;
      gap: 8px;
      font-weight: 700;
    }
    .dot {
      width: 10px;
      height: 10px;
      border-radius: 50%;
      background: var(--green);
      flex: 0 0 auto;
    }
    .dot.error { background: var(--red); }
    .transcript-text {
      min-height: 44px;
      margin-top: 8px;
      padding: 10px 12px;
      border-left: 3px solid var(--blue);
      background: #f8fafc;
      color: var(--ink);
      overflow-wrap: anywhere;
      white-space: pre-wrap;
    }
    .transcript-text.empty {
      color: var(--muted);
    }
    ul {
      list-style: none;
      margin: 0;
      padding: 0;
      display: grid;
      gap: 8px;
    }
    li {
      border: 1px solid var(--line);
      border-radius: 8px;
      padding: 12px;
      overflow-wrap: anywhere;
    }
    .device-row {
      display: flex;
      justify-content: space-between;
      gap: 12px;
      align-items: flex-start;
    }
    .device-name { font-weight: 700; }
    .device-meta {
      color: var(--muted);
      font-size: 13px;
      margin-top: 3px;
    }
    .pill {
      border: 1px solid var(--line);
      border-radius: 8px;
      padding: 2px 9px;
      font-size: 12px;
      white-space: nowrap;
    }
    .pill.online {
      border-color: rgba(24, 121, 78, 0.25);
      color: var(--green);
      background: rgba(24, 121, 78, 0.08);
    }
    .pill.offline {
      color: var(--muted);
      background: #f8fafc;
    }
    code {
      font-family: ui-monospace, "SFMono-Regular", Consolas, monospace;
      overflow-wrap: anywhere;
    }
    @media (max-width: 760px) {
      main { width: min(100% - 20px, 960px); padding: 20px 0; }
      header { display: block; }
      header button { width: 100%; margin-top: 16px; }
      .grid { grid-template-columns: 1fr; }
      .wide { grid-column: auto; }
      .pair-code { font-size: 32px; }
      .device-row { display: block; }
      .pill { display: inline-block; margin-top: 8px; }
    }
  </style>
</head>
<body>
  <main>
    <header>
      <div>
        <h1>CrossHare · 语音</h1><a href="/">← 返回照片库</a>
        <div class="muted">电脑接收端正在运行</div>
      </div>
    </header>

    <section class="grid" aria-live="polite">
      <div class="panel wide">
        <div class="label">配对状态</div>
        <div id="pairedSummary" class="value">加载中</div>
        <div id="onlineSummary" class="muted"></div>
      </div>
      <div class="panel">
        <div class="label">已同步</div>
        <div id="syncedCount" class="value">0</div>
      </div>
      <div class="panel">
        <div class="label">总大小</div>
        <div id="syncedSize" class="value">0 B</div>
      </div>
      <div class="panel full">
        <div class="label">状态</div>
        <div class="status"><span id="statusDot" class="dot"></span><span id="statusText">加载中</span></div>
        <div id="progressText" class="muted"></div>
      </div>
      <div class="panel full">
        <div class="label">语音服务</div>
        <div class="status"><span id="speechDot" class="dot"></span><span id="speechText">加载中</span></div>
        <div id="speechMeta" class="muted"></div>
      </div>
      <div class="panel full">
        <div class="label">识别准备</div>
        <div class="status"><span id="readyDot" class="dot"></span><span id="readyText">加载中</span></div>
        <div id="readyMeta" class="muted"></div>
      </div>
      <div class="panel full">
        <div class="label">麦克风传输</div>
        <div class="status"><span id="audioDot" class="dot"></span><span id="audioText">加载中</span></div>
        <div id="audioMeta" class="muted"></div>
      </div>
      <div class="panel full">
        <div class="label">语音识别</div>
        <div class="status"><span id="transcriptDot" class="dot"></span><span id="transcriptStatus">加载中</span></div>
        <div id="transcriptText" class="transcript-text empty">等待识别内容</div>
      </div>
      <div class="panel full">
        <div class="section-head">
          <div>
            <div class="label">设备</div>
            <div class="muted">手机会主动连接这台电脑并上传新文件。</div>
          </div>
          <span id="deviceCount" class="pill offline">0 台</span>
        </div>
        <ul id="deviceList"><li class="muted">暂无设备</li></ul>
      </div>
      <div class="panel full">
        <div class="label">同步目录</div>
        <code id="targetFolder"></code>
      </div>
      <div id="pairPanel" class="panel full">
        <div class="label">添加新手机</div>
        <div id="pairCode" class="value pair-code secondary">------</div>
        <div id="pairHint" class="muted">只在首次配对或添加另一台手机时需要。</div>
      </div>
    </section>

    <p class="muted">UI 仅允许本机访问。手机配对和上传使用同一个端口。</p>
  </main>

  <script>
    const pairCode = document.querySelector("#pairCode");
    const pairedSummary = document.querySelector("#pairedSummary");
    const onlineSummary = document.querySelector("#onlineSummary");
    const syncedCount = document.querySelector("#syncedCount");
    const syncedSize = document.querySelector("#syncedSize");
    const statusText = document.querySelector("#statusText");
    const statusDot = document.querySelector("#statusDot");
    const progressText = document.querySelector("#progressText");
    const speechText = document.querySelector("#speechText");
    const speechDot = document.querySelector("#speechDot");
    const speechMeta = document.querySelector("#speechMeta");
    const readyText = document.querySelector("#readyText");
    const readyDot = document.querySelector("#readyDot");
    const readyMeta = document.querySelector("#readyMeta");
    const audioText = document.querySelector("#audioText");
    const audioDot = document.querySelector("#audioDot");
    const audioMeta = document.querySelector("#audioMeta");
    const transcriptDot = document.querySelector("#transcriptDot");
    const transcriptStatus = document.querySelector("#transcriptStatus");
    const transcriptText = document.querySelector("#transcriptText");
    const targetFolder = document.querySelector("#targetFolder");
    const deviceList = document.querySelector("#deviceList");
    const deviceCount = document.querySelector("#deviceCount");
    const pairPanel = document.querySelector("#pairPanel");
    const pairHint = document.querySelector("#pairHint");

    function formatBytes(bytes) {
      const units = ["B", "KB", "MB", "GB", "TB"];
      let value = Number(bytes || 0);
      let unit = 0;
      while (value >= 1024 && unit < units.length - 1) {
        value /= 1024;
        unit += 1;
      }
      return value.toFixed(unit === 0 ? 0 : 1) + " " + units[unit];
    }

    function formatAgo(ms) {
      if (!ms && ms !== 0) return "";
      const seconds = Math.max(0, Math.round(ms / 1000));
      if (seconds < 60) return "刚刚连接";
      const minutes = Math.round(seconds / 60);
      if (minutes < 60) return minutes + " 分钟前连接";
      const hours = Math.round(minutes / 60);
      if (hours < 24) return hours + " 小时前连接";
      return Math.round(hours / 24) + " 天前连接";
    }

    function appendDevice(device, paired) {
      const item = document.createElement("li");
      const row = document.createElement("div");
      row.className = "device-row";

      const left = document.createElement("div");
      const name = document.createElement("div");
      name.className = "device-name";
      name.textContent = device.device_name || "未命名设备";
      left.append(name);

      const details = [];
      if (device.last_ip) details.push(device.last_ip);
      if (device.last_seen_ms || device.last_seen_ms === 0) details.push(formatAgo(device.last_seen_ms));
      details.push("ID " + (device.device_id || "未知"));

      const meta = document.createElement("div");
      meta.className = "device-meta";
      meta.textContent = details.join(" · ");
      left.append(meta);

      const pill = document.createElement("span");
      const recent = Boolean(device.recent);
      pill.className = "pill " + (recent ? "online" : "offline");
      pill.textContent = paired ? (recent ? "最近连接" : "等待手机连接") : "未配对";

      row.append(left, pill);
      item.append(row);
      deviceList.append(item);
    }

    function renderDevices(devices) {
      deviceList.innerHTML = "";
      const paired = devices || [];
      if (paired.length === 0) {
        const item = document.createElement("li");
        item.className = "muted";
        item.textContent = "还没有配对手机。先在手机上输入下面的配对码。";
        deviceList.append(item);
        return;
      }
      paired.forEach((device) => appendDevice(device, true));
    }

    function renderSpeech(speech) {
      const ready = Boolean(speech.ready);
      speechDot.classList.toggle("error", !ready);
      if (ready) {
        speechText.textContent = "实时语音服务已就绪";
        speechMeta.textContent = [
          speech.realtime_url || "",
          speech.realtime_model || "",
          serviceLabel(speech, "gateway"),
          serviceLabel(speech, "stt-realtime")
        ].filter(Boolean).join(" · ");
        return;
      }

      if (speech.error) {
        speechText.textContent = "实时语音服务不可用";
        speechMeta.textContent = speech.error;
      } else if (speech.auto_start_attempted && speech.auto_start_error) {
        speechText.textContent = "实时语音服务启动失败";
        speechMeta.textContent = speech.auto_start_error;
      } else if (speech.auto_start_attempted) {
        speechText.textContent = "等待实时语音服务就绪";
        speechMeta.textContent = speech.status || "正在检查服务状态";
      } else {
        speechText.textContent = "正在启动实时语音服务";
        speechMeta.textContent = speech.status || "等待 localman 返回状态";
      }
    }

    function serviceLabel(speech, name) {
      const service = speech.services && speech.services[name];
      if (!service) return "";
      return name + " " + (service.running ? "运行中" : (service.status || "未运行"));
    }

    function renderReadiness(devices, recentCount, speech, audio) {
      const paired = devices.length > 0;
      const speechReady = Boolean(speech.ready);
      const ready = paired && speechReady;
      readyDot.classList.toggle("error", !ready);

      if (!paired) {
        readyText.textContent = "需要先配对手机";
        readyMeta.textContent = "配对码 " + (pairCode.textContent || "------");
      } else if (!speechReady) {
        readyText.textContent = "等待语音服务";
        readyMeta.textContent = speech.error || speech.auto_start_error || "服务启动后即可识别";
      } else if (audio.active) {
        readyText.textContent = "正在识别 " + (audio.device_name || "手机") + " 的语音";
        readyMeta.textContent = formatBytes(audio.bytes_received || 0) + " 已接收";
      } else if (recentCount > 0) {
        readyText.textContent = "准备识别";
        readyMeta.textContent = "手机已连接，开始麦克风传输后会显示识别内容";
      } else {
        readyText.textContent = "手机已配对，等待连接";
        readyMeta.textContent = "在手机端启动麦克风传输";
      }
    }

    async function refresh() {
      try {
        const response = await fetch("/api/ui/status", { cache: "no-store" });
        if (!response.ok) throw new Error(await response.text());
        const data = await response.json();
        const devices = data.paired_devices || [];
        const recentCount = devices.filter((device) => device.recent).length;

        pairCode.textContent = data.pair_code || "------";
        pairedSummary.textContent = devices.length > 0 ? devices.length + " 台已配对" : "未配对";
        onlineSummary.textContent = devices.length > 0
          ? recentCount + " 台最近连接，" + (devices.length - recentCount) + " 台等待手机发起同步"
          : "首次连接需要在手机上输入下面的配对码。";
        deviceCount.textContent = devices.length + " 台已配对";
        deviceCount.className = "pill " + (recentCount > 0 ? "online" : "offline");
        pairPanel.style.order = devices.length > 0 ? "10" : "-1";
        pairHint.textContent = devices.length > 0
          ? "只在添加另一台手机时需要，已配对手机会继续使用保存的令牌。"
          : "只在首次配对时需要，配对成功后手机会保存连接。";

        syncedCount.textContent = data.synced_count ?? 0;
        syncedSize.textContent = formatBytes(data.synced_size);
        targetFolder.textContent = data.target_folder || "";
        renderDevices(devices);

        const progress = data.progress || {};
        const isError = Boolean(progress.error_message);
        statusDot.classList.toggle("error", isError);
        statusText.textContent = isError ? progress.error_message : (progress.status || "未知");
        if (progress.current_file) {
          progressText.textContent = progress.current_file + " " + (progress.files_done || 0) + "/" + (progress.files_total || 0);
        } else {
          progressText.textContent = "";
        }

        const audio = data.audio || {};
        const speech = data.speech || {};
        renderSpeech(speech);
        renderReadiness(devices, recentCount, speech, audio);

        audioDot.classList.toggle("error", Boolean(audio.last_error || audio.transcript_error));
        if (audio.active) {
          audioText.textContent = "正在接收 " + (audio.device_name || "手机") + " 的麦克风";
          audioMeta.textContent = [
            audio.current_file || "",
            formatBytes(audio.bytes_received || 0),
            audio.sample_rate ? (audio.sample_rate + " Hz") : "",
            audio.channels ? (audio.channels + " ch") : ""
          ].filter(Boolean).join(" · ");
        } else if (audio.transcript_error) {
          audioText.textContent = "实时转写异常";
          audioMeta.textContent = "";
        } else if (audio.last_error) {
          audioText.textContent = audio.last_error;
          audioMeta.textContent = "";
        } else if (audio.last_completed_file) {
          audioText.textContent = "最近一次录音已保存";
          audioMeta.textContent = [
            audio.last_completed_file,
            audio.last_completed_at
          ].filter(Boolean).join(" · ");
        } else {
          audioText.textContent = "等待手机开始麦克风传输";
          audioMeta.textContent = "";
        }

        const transcriptValue = audio.transcript_partial || audio.transcript_final || audio.last_transcript || "";
        transcriptDot.classList.toggle("error", Boolean(audio.transcript_error));
        transcriptText.classList.toggle("empty", !transcriptValue);
        if (audio.transcript_error) {
          transcriptStatus.textContent = "识别异常";
          transcriptText.textContent = audio.transcript_error;
        } else if (audio.transcript_partial) {
          transcriptStatus.textContent = "正在识别";
          transcriptText.textContent = transcriptValue;
        } else if (audio.transcript_final || audio.last_transcript) {
          transcriptStatus.textContent = audio.active ? "已生成片段" : "最近识别结果";
          transcriptText.textContent = transcriptValue;
        } else {
          transcriptStatus.textContent = audio.active ? "等待模型返回" : "等待语音输入";
          transcriptText.textContent = "等待识别内容";
        }
      } catch (error) {
        statusDot.classList.add("error");
        statusText.textContent = "无法读取本机 UI 状态";
        progressText.textContent = String(error.message || error);
        audioDot.classList.add("error");
        audioText.textContent = "无法读取麦克风状态";
        audioMeta.textContent = "";
        speechDot.classList.add("error");
        speechText.textContent = "无法读取语音服务状态";
        speechMeta.textContent = String(error.message || error);
        readyDot.classList.add("error");
        readyText.textContent = "无法判断识别准备状态";
        readyMeta.textContent = "";
        transcriptDot.classList.add("error");
        transcriptStatus.textContent = "无法读取识别状态";
        transcriptText.classList.add("empty");
        transcriptText.textContent = "";
      }
    }

    refresh();
    setInterval(refresh, 3000);
  </script>
</body>
</html>`))
