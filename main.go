package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"text/template"
	"time" // 引入 time 包用于超时控制
)

// --- 配置变量 ---
var (
	listenAddr string // 监听地址和端口
	username   string // Web 访问用户名
	password   string // Web 访问密码
)

// --- HTML 模板 (已更新) ---
const indexHTML = `
<!DOCTYPE html>
<html lang="zh-CN">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>远程控制</title>
    <style>
        body { font-family: sans-serif; display: flex; flex-direction: column; /* 改为纵向排列 */ justify-content: center; align-items: center; min-height: 100vh; background-color: #f4f4f4; }
        .container { background: white; padding: 20px; margin-bottom: 20px; /* 容器间加点间距 */ border-radius: 8px; box-shadow: 0 2px 10px rgba(0,0,0,0.1); text-align: center; width: 90%; max-width: 500px; }
        h1 { margin-top: 0; }
        button, input[type=range] { margin: 8px 5px; padding: 10px 15px; font-size: 16px; cursor: pointer; border: none; border-radius: 4px; }
        button { background-color: #007bff; color: white; }
        button:hover { background-color: #0056b3; }
        button.mute { background-color: #ffc107; }
        button.mute:hover { background-color: #e0a800; }
        button.unmute { background-color: #28a745; }
        button.unmute:hover { background-color: #218838; }
        button.playpause-play { background-color: #28a745; } /* 播放按钮样式 */
        button.playpause-play:hover { background-color: #218838; }
        button.playpause-pause { background-color: #ffc107; } /* 暂停按钮样式 */
        button.playpause-pause:hover { background-color: #e0a800; }
        #volumeSlider { width: 80%; }
        #currentInfo { margin-top: 15px; font-size: 18px; font-weight: bold; }
        #mediaInfo { margin-top: 10px; font-size: 14px; color: #555; min-height: 40px; /* 给点最小高度避免跳动 */ word-wrap: break-word; }
        #status { margin-top: 10px; color: green; font-style: italic; min-height: 1.2em; }
        #error { margin-top: 10px; color: red; font-weight: bold; min-height: 1.2em; }
        .control-section div { margin-bottom: 10px; }
    </style>
</head>
<body>
    <div class="container control-section">
        <h1>音量控制</h1>
        <div>
            <button onclick="sendVolumeCommand('down')">音量减 (-5%)</button>
            <button onclick="sendVolumeCommand('up')">音量加 (+5%)</button>
            <button id="muteButton" onclick="sendVolumeCommand('toggleMute')">切换静音</button>
        </div>
        <div>
            <input type="range" id="volumeSlider" min="0" max="150" step="1" onchange="setVolume()">
        </div>
        <div id="currentVolume">加载中...</div>
    </div>

    <div class="container control-section">
        <h1>媒体控制</h1>
        <div id="mediaInfo">播放器信息加载中...</div>
         <div>
            <button id="playPauseButton" onclick="sendMediaCommand('play-pause')">播放/暂停</button>
            <button onclick="sendMediaCommand('next')">下一曲</button>
            <button onclick="sendMediaCommand('previous')">上一曲</button> </div>
    </div>

    <div class="container">
         <div id="status"></div>
         <div id="error"></div>
    </div>

    <script>
        const statusEl = document.getElementById('status');
        const errorEl = document.getElementById('error');
        // 音量元素
        const volumeDisplay = document.getElementById('currentVolume');
        const slider = document.getElementById('volumeSlider');
        const muteButton = document.getElementById('muteButton');
        // 媒体元素
        const mediaInfoEl = document.getElementById('mediaInfo');
        const playPauseButton = document.getElementById('playPauseButton');

        let volumeDebounceTimer;
        let currentVolume = -1;
        let currentMuted = null;
        let currentMediaStatus = null;

        // Base64 编码的认证信息 (由 Go 模板填充)
        const authHeader = 'Basic ' + btoa('{{.Username}}' + ':' + '{{.Password}}');

        // 通用发送命令函数
        async function sendCommand(url, action, value = null, method = 'POST') {
            statusEl.textContent = '正在发送命令...';
            errorEl.textContent = '';
            const fullUrl = url + action;
            const options = {
                method: method,
                headers: { 'Authorization': authHeader }
            };
            if (value !== null && method === 'POST') {
                options.headers['Content-Type'] = 'application/json';
                options.body = JSON.stringify({ value: value });
            }

            try {
                const response = await fetch(fullUrl, options);
                const responseText = await response.text(); // 获取原始文本以供调试
                if (!response.ok) {
                    // 尝试解析错误信息，如果失败则使用原始文本
                    let errorMsg = responseText;
                    try {
                        const errorJson = JSON.parse(responseText);
                        errorMsg = errorJson.error || JSON.stringify(errorJson);
                    } catch(e) { /* Ignore parsing error, use raw text */ }
                    throw new Error('HTTP 错误! 状态: ' + response.status + ', 消息: ' + errorMsg);
                }
                // 尝试解析 JSON，如果响应体为空或非 JSON，则认为成功
                let result = { message: '命令发送成功!'};
                 try {
                    if (responseText.trim()) {
                        result = JSON.parse(responseText);
                    }
                 } catch(e) {
                     console.warn("Response was not valid JSON, assuming success:", responseText);
                 }

                statusEl.textContent = result.message || '命令发送成功!';
                await fetchAllStatus(); // 命令成功后更新所有状态
                return result; // 返回结果供特定处理使用
            } catch (error) {
                console.error('发送命令时出错:', error);
                errorEl.textContent = '错误: ' + error.message;
                statusEl.textContent = '';
                throw error; // 重新抛出错误，让调用者知道失败了
            }
        }

        // 发送音量命令
        function sendVolumeCommand(action, value = null) {
            sendCommand('/volume/', action, value);
        }

        // 发送媒体命令
        function sendMediaCommand(action) {
            sendCommand('/media/control/', action);
        }

        // 使用滑块设置特定音量 (带防抖)
        function setVolume() {
            clearTimeout(volumeDebounceTimer);
            volumeDebounceTimer = setTimeout(() => {
                const volume = slider.value;
                sendVolumeCommand('set', parseInt(volume, 10));
            }, 250);
        }

        // 更新音量 UI
        function updateVolumeUI(data) {
            if (data && typeof data.volume === 'number' && data.volume !== currentVolume) {
                currentVolume = data.volume;
                volumeDisplay.textContent = '当前音量: ' + data.volume + '%';
                slider.value = data.volume; // Sync slider
            }
             if (data && typeof data.muted === 'boolean' && data.muted !== currentMuted) {
                 currentMuted = data.muted;
                 if (data.muted) {
                     muteButton.textContent = '取消静音';
                     muteButton.classList.remove('mute');
                     muteButton.classList.add('unmute');
                     if (volumeDisplay.textContent.indexOf('(已静音)') === -1) {
                       volumeDisplay.textContent += ' (已静音)';
                     }
                 } else {
                     muteButton.textContent = '静音';
                     muteButton.classList.remove('unmute');
                     muteButton.classList.add('mute');
                     volumeDisplay.textContent = volumeDisplay.textContent.replace(' (已静音)', '');
                 }
            }
             if(data && !data.volume && !data.muted) {
                 volumeDisplay.textContent = '无法加载音量状态';
             }
        }

        // 更新媒体 UI
        function updateMediaUI(data) {
             if (!data || data.error || !data.title) {
                 mediaInfoEl.textContent = data && data.error ? data.error : '没有媒体在播放或无法获取信息';
                 playPauseButton.textContent = '播放/暂停';
                 playPauseButton.classList.remove('playpause-play', 'playpause-pause');
                 currentMediaStatus = null;
             } else {
                 let infoText = data.title;
                 if (data.artist) infoText += ' - ' + data.artist;
                 if (data.album) infoText += ' (' + data.album + ')';
                 if (data.playerName) infoText = '[' + data.playerName + '] ' + infoText;
                 mediaInfoEl.textContent = infoText;
                 currentMediaStatus = data.status; // Store status (e.g., "Playing", "Paused")

                 if (data.status === 'Playing') {
                     playPauseButton.textContent = '暂停';
                     playPauseButton.classList.remove('playpause-play');
                     playPauseButton.classList.add('playpause-pause');
                 } else { // Paused or Stopped
                     playPauseButton.textContent = '播放';
                     playPauseButton.classList.remove('playpause-pause');
                     playPauseButton.classList.add('playpause-play');
                 }
             }
        }

        // 获取所有状态（音量和媒体）
        async function fetchAllStatus() {
            errorEl.textContent = ''; // Clear previous errors
            statusEl.textContent = '正在获取状态...';
            let volumeOk = false;
            let mediaOk = false;

            // 获取音量状态
            try {
                const volResponse = await fetch('/volume/status', { headers: { 'Authorization': authHeader } });
                if (!volResponse.ok) throw new Error('HTTP ' + volResponse.status);
                const volData = await volResponse.json();
                updateVolumeUI(volData);
                volumeOk = true;
            } catch (error) {
                console.error('获取音量状态时出错:', error);
                errorEl.textContent += '获取音量状态错误. ';
                updateVolumeUI(null); // Indicate error in UI
            }

            // 获取媒体状态
            try {
                const mediaResponse = await fetch('/media/status', { headers: { 'Authorization': authHeader } });
                // playerctl might return 404 if no player found, which is okay
                if (!mediaResponse.ok && mediaResponse.status !== 404) {
                     throw new Error('HTTP ' + mediaResponse.status);
                }
                const mediaData = await mediaResponse.json();
                updateMediaUI(mediaData);
                mediaOk = true;
            } catch (error) {
                console.error('获取媒体状态时出错:', error);
                errorEl.textContent += '获取媒体状态错误. ';
                updateMediaUI({ error: '获取媒体状态时出错' }); // Show error in media section
            }

            if (volumeOk && mediaOk) {
                statusEl.textContent = '状态已更新 (' + new Date().toLocaleTimeString() + ')';
            } else {
                 statusEl.textContent = '部分状态更新失败';
            }
        }

        // 页面加载时获取初始状态，并设置定时刷新
        fetchAllStatus();
        setInterval(fetchAllStatus, 5000); // 每 5 秒刷新一次状态
    </script>
</body>
</html>
`

// --- 认证中间件 (与之前相同) ---
func basicAuth(handler http.HandlerFunc, requiredUser, requiredPassword string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user, pass, ok := r.BasicAuth()
		if !ok || user != requiredUser || pass != requiredPassword {
			w.Header().Set("WWW-Authenticate", `Basic realm="Restricted"`)
			// 返回 JSON 错误而不是纯文本
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			json.NewEncoder(w).Encode(map[string]string{"error": "Unauthorized"})
			log.Printf("Auth failed for user '%s' from %s", user, r.RemoteAddr)
			return
		}
		handler(w, r)
	}
}

// --- 封装执行命令的函数 ---
// 添加了超时控制
func executeCommand(timeout time.Duration, name string, args ...string) ([]byte, error) {
	cmd := exec.Command(name, args...)

	// 设置一个通道，用于接收命令完成的信号或错误
	done := make(chan error, 1)
	var output []byte
	var err error

	go func() {
		output, err = cmd.CombinedOutput() // 同时获取 stdout 和 stderr
		done <- err
	}()

	// 等待命令完成或超时
	select {
	case <-time.After(timeout):
		// 超时，尝试杀死进程
		if cmd.Process != nil {
			if errKill := cmd.Process.Kill(); errKill != nil {
				log.Printf("Failed to kill command %s after timeout: %v", name, errKill)
			}
		}
		return nil, fmt.Errorf("command %s timed out after %v", name, timeout)
	case err = <-done:
		// 命令完成（可能成功也可能失败）
		if err != nil {
			// 如果命令执行失败，将输出也包含在错误信息中返回
			return output, fmt.Errorf("command %s failed: %v, output: %s", name, err, string(output))
		}
		return output, nil
	}
}

// --- 音量处理函数 (与之前类似，但使用 executeCommand) ---
func handleVolume(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}
	parts := strings.Split(r.URL.Path, "/")
	if len(parts) < 3 {
		http.Error(w, "Bad Request: Missing action", http.StatusBadRequest)
		return
	}
	action := parts[2]

	var pactlArgs []string
	var successMsg string

	switch action {
	case "up":
		pactlArgs = []string{"set-sink-volume", "@DEFAULT_SINK@", "+5%"}
		successMsg = "音量已增加"
	case "down":
		pactlArgs = []string{"set-sink-volume", "@DEFAULT_SINK@", "-5%"}
		successMsg = "音量已降低"
	case "toggleMute":
		pactlArgs = []string{"set-sink-mute", "@DEFAULT_SINK@", "toggle"}
		successMsg = "静音状态已切换"
	case "set":
		var payload struct {
			Value int `json:"value"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			http.Error(w, "Bad Request: Invalid JSON payload", http.StatusBadRequest)
			return
		}
		if payload.Value < 0 || payload.Value > 150 {
			http.Error(w, "Bad Request: Volume must be between 0 and 150", http.StatusBadRequest)
			return
		}
		volumeArg := fmt.Sprintf("%d%%", payload.Value)
		pactlArgs = []string{"set-sink-volume", "@DEFAULT_SINK@", volumeArg}
		successMsg = fmt.Sprintf("音量已设置为 %d%%", payload.Value)
	default:
		http.Error(w, "Bad Request: Unknown action", http.StatusBadRequest)
		return
	}

	_, err := executeCommand(2*time.Second, "pactl", pactlArgs...) // 2 秒超时
	if err != nil {
		log.Printf("Error executing pactl command: %v", err) // 错误信息已包含 output
		http.Error(w, fmt.Sprintf("Failed to execute pactl command: %v", err), http.StatusInternalServerError)
		return
	}

	log.Printf("Executed: pactl %s", strings.Join(pactlArgs, " "))
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"message": successMsg})
}

// --- 音量状态处理 (与之前类似，但使用 executeCommand) ---
func handleVolumeStatus(w http.ResponseWriter, r *http.Request) {
	// Get Volume
	outVol, errVol := executeCommand(2*time.Second, "pactl", "get-sink-volume", "@DEFAULT_SINK@")
	if errVol != nil {
		log.Printf("Error getting volume: %v", errVol)
		http.Error(w, "Failed to get volume", http.StatusInternalServerError)
		return
	}

	// Get Mute Status
	outMute, errMute := executeCommand(2*time.Second, "pactl", "get-sink-mute", "@DEFAULT_SINK@")
	if errMute != nil {
		log.Printf("Error getting mute status: %v", errMute)
		http.Error(w, "Failed to get mute status", http.StatusInternalServerError)
		return
	}

	// Parse Volume
	reVol := regexp.MustCompile(`(\d+)%`)
	matchesVol := reVol.FindStringSubmatch(string(outVol))
	volume := 0
	if len(matchesVol) > 1 {
		volume, _ = strconv.Atoi(matchesVol[1])
	} else {
		log.Printf("Could not parse volume from output: %s", string(outVol))
	}

	// Parse Mute Status
	muted := strings.Contains(strings.ToLower(string(outMute)), "yes")

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"volume": volume,
		"muted":  muted,
	})
}

// --- 新增：媒体控制处理 ---
func handleMediaControl(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}
	parts := strings.Split(r.URL.Path, "/")
	if len(parts) < 4 { // Expect /media/control/{action}
		http.Error(w, "Bad Request: Missing action", http.StatusBadRequest)
		return
	}
	action := parts[3] // Action is the 4th part

	var playerctlArgs []string
	var successMsg string

	switch action {
	case "play-pause":
		playerctlArgs = []string{"play-pause"}
		successMsg = "播放/暂停状态已切换"
	case "next":
		playerctlArgs = []string{"next"}
		successMsg = "已切换到下一曲"
	case "previous":
		playerctlArgs = []string{"previous"}
		successMsg = "已切换到上一曲"
	case "stop": // Optional: Add stop functionality if needed
		playerctlArgs = []string{"stop"}
		successMsg = "已停止播放"
	default:
		http.Error(w, "Bad Request: Unknown media action", http.StatusBadRequest)
		return
	}

	output, err := executeCommand(2*time.Second, "playerctl", playerctlArgs...) // 2 秒超时
	if err != nil {
		errMsg := fmt.Sprintf("Failed to execute playerctl command: %v", err)
		log.Print(errMsg) // 修复点：改为 log.Print
		// 检查是否是 "No players found" 错误
		if strings.Contains(string(output), "No players found") || strings.Contains(err.Error(), "No players found") {
			http.Error(w, `{"error": "没有找到活动的播放器"}`, http.StatusNotFound) // 返回 404
		} else {
			http.Error(w, fmt.Sprintf(`{"error": "%s"}`, errMsg), http.StatusInternalServerError)
		}
		return
	}

	log.Printf("Executed: playerctl %s", strings.Join(playerctlArgs, " "))
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"message": successMsg})
}

// --- 新增：媒体状态处理 ---
func handleMediaStatus(w http.ResponseWriter, r *http.Request) {
	// 使用 playerctl metadata --format 获取 JSON 输出，简化解析
	format := `{"playerName":"{{playerName}}", "title":"{{markup_escape(title)}}", "artist":"{{markup_escape(artist)}}", "album":"{{markup_escape(album)}}", "status":"{{status}}"}`
	args := []string{"metadata", "--format", format}

	output, err := executeCommand(3*time.Second, "playerctl", args...) // 3 秒超时

	// 检查 playerctl 是否因为没有播放器而退出
	// playerctl 在找不到播放器时通常会返回非零退出码，并可能在 stderr 输出 "No players found" 或类似信息
	if err != nil {
		errStr := err.Error()
		if strings.Contains(errStr, "No players found") || strings.Contains(errStr, "No player could handle this command") || strings.Contains(string(output), "No players found") {
			// 这是正常情况，表示没有播放器在运行
			log.Println("No active media player found.")
			w.Header().Set("Content-Type", "application/json")
			// 返回特定的 JSON 表示没有播放器，或者返回 404 也可以
			// http.Error(w, `{"error": "没有找到活动的播放器"}`, http.StatusNotFound)
			json.NewEncoder(w).Encode(map[string]string{"status": "Stopped", "title": "", "error": "没有找到活动的播放器"})
			return
		}
		// 其他错误
		log.Printf("Error getting media status: %v", err) // 错误已包含输出
		http.Error(w, fmt.Sprintf(`{"error": "获取媒体状态失败: %v"}`, err), http.StatusInternalServerError)
		return
	}

	// playerctl 成功执行，输出应该是 JSON 格式
	w.Header().Set("Content-Type", "application/json")
	// 直接将 playerctl 的 JSON 输出写入响应
	_, writeErr := w.Write(output)
	if writeErr != nil {
		log.Printf("Error writing media status response: %v", writeErr)
	}
}

func handleRoot(w http.ResponseWriter, r *http.Request) {
	// 使用基本认证中间件保护根路径
	basicAuth(func(w http.ResponseWriter, r *http.Request) {
		// 定义模板数据
		templateData := struct {
			Username string
			Password string
		}{
			Username: username,
			Password: password,
		}

		// 渲染 HTML 模板并注入认证信息
		tmpl, err := template.New("index").Parse(indexHTML)
		if err != nil {
			log.Printf("Error parsing HTML template: %v", err)
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}

		// 执行模板并写入响应
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if err := tmpl.Execute(w, templateData); err != nil {
			log.Printf("Error executing HTML template: %v", err)
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		}
	}, username, password)(w, r)
}

// --- 主函数 (已更新路由) ---
func main() {
	flag.StringVar(&listenAddr, "addr", ":8080", "监听的地址和端口")
	flag.StringVar(&username, "user", "admin", "Web 访问的用户名")
	flag.StringVar(&password, "pass", "password", "Web 访问的密码 (强烈建议修改!)")
	flag.Parse()

	if password == "password" {
		log.Println("警告: 正在使用默认密码 'password'。请使用 -pass 参数设置一个强密码。")
	}

	// 注册 HTTP 路由处理函数
	http.HandleFunc("/", handleRoot) // Auth is applied inside handleRoot

	// 音量相关路由 (添加 /status 子路径)
	http.HandleFunc("/volume/status", basicAuth(handleVolumeStatus, username, password))
	http.HandleFunc("/volume/", basicAuth(handleVolume, username, password)) // 这个要放在后面，避免匹配 /volume/status

	// 媒体相关路由
	http.HandleFunc("/media/status", basicAuth(handleMediaStatus, username, password))
	http.HandleFunc("/media/control/", basicAuth(handleMediaControl, username, password)) // 匹配 /media/control/{action}

	log.Printf("服务器正在启动，监听地址: %s", listenAddr)
	log.Printf("请使用用户名: %s 访问", username)
	log.Printf("请使用通过 -pass 参数提供的密码 (或默认密码 'password')")
	log.Fatal(http.ListenAndServe(listenAddr, nil))
}
