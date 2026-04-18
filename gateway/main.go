package main

import (
	"bytes"
	_ "embed"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	"github.com/shirou/gopsutil/v3/cpu"
	psnet "github.com/shirou/gopsutil/v3/net"
)

type CameraConfig struct {
	Name      string `json:"name"`
	LocalRTSP string `json:"local_rtsp"`
	VPSPort   int    `json:"vps_port"`
}

type Config struct {
	ServerURL       string         `json:"server_url"`
	CloudflareToken string         `json:"cloudflare_token"`
	ApiUsername     string         `json:"api_username"`
	ApiPassword     string         `json:"api_password"`
	VPNMode         string         `json:"vpn_mode"`
	StreamEngine    string         `json:"stream_engine"` // "go2rtc", "mediamtx", "ffmpeg"
	L2TPServer      string         `json:"l2tp_server"`
	L2TPUser        string         `json:"l2tp_user"`
	L2TPPass        string         `json:"l2tp_pass"`
	L2TPPSK         string         `json:"l2tp_psk"`
	Cameras         []CameraConfig `json:"cameras"`
}

type TunnelInstance struct {
	Camera     CameraConfig
	Running    bool
	Status     string
	StatusDot  *canvas.Circle
	StatusText *widget.Label
	ToggleBtn  *widget.Button
	StopChan   chan bool
	mu         sync.Mutex
	Card       fyne.CanvasObject
	proxyLn    net.Listener
	proxyPort  int
}

var singleInstanceListener net.Listener

func checkSingleInstance() {
	ln, err := net.Listen("tcp", "127.0.0.1:48231")
	if err != nil {
		fmt.Println("Application is already running. Exiting...")
		os.Exit(0)
	}
	singleInstanceListener = ln
}

var (
	instances     []*TunnelInstance
	config        *Config
	configPath    = "config.json"
	globalLogs    *widget.Entry
	logData       []string
	logMu         sync.Mutex
	myApp         fyne.App
	myWindow      fyne.Window
	listContainer *fyne.Container
	lblMonitor    *widget.Label
	trayMenu      *fyne.Menu
	trayCPUItem   *fyne.MenuItem
	trayUpItem    *fyne.MenuItem
	trayDownItem  *fyne.MenuItem
)

func main() {
	checkSingleInstance()

	myApp = app.NewWithID("com.rtsp2go.gateway")
	myWindow = myApp.NewWindow("RTSP2go Gateway")

	myApp.SetIcon(resourceAppIconPng)
	myWindow.SetIcon(resourceAppIconPng)

	// Load Config
	err := loadConfig()
	if err != nil {
		// Create default config if not found
		config = &Config{
			ServerURL: "https://stream.campod.my.id",
			VPNMode:   "zerotier",
			Cameras:   []CameraConfig{},
		}
		saveConfig()
	}

	setupSystemTray()
	startMonitoring()

	// Tabs
	tabs := container.NewAppTabs(
		container.NewTabItemWithIcon("Dashboard", theme.HomeIcon(), buildDashboard()),
		container.NewTabItemWithIcon("Settings", theme.SettingsIcon(), buildSettings()),
	)
	tabs.SetTabLocation(container.TabLocationTop)

	myWindow.SetContent(tabs)
	myWindow.Resize(fyne.NewSize(550, 750))

	// Set a minimum size so the window never shrinks to a tiny unusable bar
	myWindow.Canvas().Content().Resize(fyne.NewSize(550, 750))

	// Intercept close to minimize to tray
	myWindow.SetCloseIntercept(func() {
		myWindow.Hide()
		addLog("App minimized to system tray. Tunnels are still actively managed.")
	})

	myWindow.ShowAndRun()
}

func setupSystemTray() {
	if desk, ok := myApp.(desktop.App); ok {
		trayCPUItem = fyne.NewMenuItem("💻 CPU: -- idle --", nil)
		trayCPUItem.Disabled = true
		trayUpItem = fyne.NewMenuItem("🔼 Up: -- idle --", nil)
		trayUpItem.Disabled = true
		trayDownItem = fyne.NewMenuItem("🔽 Down: -- idle --", nil)
		trayDownItem.Disabled = true

		trayMenu = fyne.NewMenu("MyApp",
			trayCPUItem,
			trayUpItem,
			trayDownItem,
			fyne.NewMenuItemSeparator(),
			fyne.NewMenuItem("Show Window", func() {
				myWindow.Show()
			}),
			fyne.NewMenuItem("Start All", func() {
				startAll()
			}),
			fyne.NewMenuItem("Stop All", func() {
				stopAll()
			}),
			fyne.NewMenuItemSeparator(),
			fyne.NewMenuItem("Quit", func() {
				cleanup()
				myApp.Quit()
				os.Exit(0)
			}))
		desk.SetSystemTrayMenu(trayMenu)
	}
}

func buildDashboard() fyne.CanvasObject {
	lblMonitor = widget.NewLabelWithStyle("💻 CPU: -- | 🔼 Up: -- | 🔽 Down: --", fyne.TextAlignTrailing, fyne.TextStyle{Bold: true})

	header := container.NewHBox(
		widget.NewIcon(theme.ComputerIcon()),
		widget.NewLabelWithStyle("RTSP2go GATEWAY CONTROL", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		layout.NewSpacer(),
		lblMonitor,
	)

	btnStartAll := widget.NewButtonWithIcon("Start All", theme.MediaPlayIcon(), startAll)
	btnStartAll.Importance = widget.HighImportance

	btnStopAll := widget.NewButtonWithIcon("Stop All", theme.MediaStopIcon(), stopAll)
	btnStopAll.Importance = widget.DangerImportance

	btnAddCamera := widget.NewButtonWithIcon("", theme.ContentAddIcon(), showAddCameraDialog)

	btnImportCSV := widget.NewButtonWithIcon("", theme.FileIcon(), importCSV)

	btnDiagnose := widget.NewButtonWithIcon("", theme.SearchIcon(), diagnoseVPS)

	btnOpenDash := widget.NewButtonWithIcon("", theme.ComputerIcon(), openDashboard)

	// controls uses an HBox to fit all buttons neatly
	controls := container.NewHBox(btnAddCamera, btnImportCSV, btnDiagnose, btnOpenDash, layout.NewSpacer(), btnStartAll, btnStopAll)

	listContainer = container.NewVBox()
	refreshCameraList()

	// Logs Section
	logData = []string{"System ready. Awaiting interaction..."}
	globalLogs = widget.NewMultiLineEntry()
	globalLogs.Wrapping = fyne.TextWrapWord
	// Use disabled state initially to mimic read-only, we manage the text
	// programmatically via SetText
	globalLogs.Disable()
	globalLogs.SetText(strings.Join(logData, "\n"))

	// Because Disabled entries aren't strictly selectable in some Fyne versions,
	// typically developers leave it enabled but intercept TypedRune,
	// or they just allow the user to type but it gets overwritten.
	// To make it selectable and copyable natively without custom extensions,
	// we keep it enabled but override TypedRune to do nothing, preventing typing.
	globalLogs.Enable()
	globalLogs.OnChanged = func(s string) {
		// Do nothing on user input to keep it pseudo-readonly
	}

	logScroll := container.NewStack(globalLogs)

	return container.NewBorder(
		container.NewVBox(header, widget.NewSeparator(), controls, widget.NewSeparator()),
		widget.NewLabelWithStyle("v1.2.0 - RTSP2go Project", fyne.TextAlignCenter, fyne.TextStyle{Italic: true}),
		nil, nil,
		container.NewVSplit(
			container.NewVScroll(listContainer),
			container.NewBorder(
				widget.NewLabelWithStyle("Live Activity", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
				nil, nil, nil,
				logScroll,
			),
		),
	)
}

func buildSettings() fyne.CanvasObject {
	entryURL := widget.NewEntry()
	entryURL.SetPlaceHolder("e.g., https://stream.campod.my.id")
	entryURL.SetText(config.ServerURL)

	entryCF := widget.NewEntry()
	entryCF.SetPlaceHolder("Cloudflare Tunnel Token (Optional)")
	entryCF.SetText(config.CloudflareToken)

	entryApiUser := widget.NewEntry()
	entryApiUser.SetPlaceHolder("Admin / Gateway User")
	entryApiUser.SetText(config.ApiUsername)

	entryApiPass := widget.NewPasswordEntry()
	entryApiPass.SetPlaceHolder("Password")
	entryApiPass.SetText(config.ApiPassword)

	vpnModes := []string{"ZeroTier VPN", "L2TP VPN", "None (Direct/Tunnel Only)"}
	vpnModeSelect := widget.NewSelect(vpnModes, nil)
	if config.VPNMode == "none" {
		vpnModeSelect.SetSelected("None (Direct/Tunnel Only)")
	} else if config.VPNMode == "l2tp" {
		vpnModeSelect.SetSelected("L2TP VPN")
	} else {
		vpnModeSelect.SetSelected("ZeroTier VPN")
	}

	engineModes := []string{"Go2RTC (Balanced)", "MediaMTX (Scalable)", "FFmpeg (Transcode/Pro)"}
	engineSelect := widget.NewSelect(engineModes, nil)
	switch config.StreamEngine {
	case "mediamtx":
		engineSelect.SetSelected("MediaMTX (Scalable)")
	case "ffmpeg":
		engineSelect.SetSelected("FFmpeg (Transcode/Pro)")
	default:
		engineSelect.SetSelected("Go2RTC (Balanced)")
	}

	// L2TP fields
	entryL2TPServer := widget.NewEntry()
	entryL2TPServer.SetPlaceHolder("e.g., 43.157.204.11")
	entryL2TPServer.SetText(config.L2TPServer)

	entryL2TPUser := widget.NewEntry()
	entryL2TPUser.SetPlaceHolder("e.g., vpnuser")
	entryL2TPUser.SetText(config.L2TPUser)

	entryL2TPPass := widget.NewPasswordEntry()
	entryL2TPPass.SetPlaceHolder("VPN Password")
	entryL2TPPass.SetText(config.L2TPPass)

	entryL2TPPSK := widget.NewPasswordEntry()
	entryL2TPPSK.SetPlaceHolder("Pre-Shared Key")
	entryL2TPPSK.SetText(config.L2TPPSK)

	l2tpSection := container.NewVBox(
		widget.NewSeparator(),
		widget.NewLabelWithStyle("L2TP VPN Configuration", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		widget.NewForm(
			widget.NewFormItem("L2TP Server IP", entryL2TPServer),
			widget.NewFormItem("L2TP Username", entryL2TPUser),
			widget.NewFormItem("L2TP Password", entryL2TPPass),
			widget.NewFormItem("L2TP PSK", entryL2TPPSK),
		),
		widget.NewLabelWithStyle("Note: L2TP will auto-connect when you click Start All.", fyne.TextAlignLeading, fyne.TextStyle{Italic: true}),
	)

	ztSection := container.NewVBox(
		widget.NewSeparator(),
		widget.NewLabelWithStyle("ZeroTier Configuration", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		widget.NewLabelWithStyle("Ensure ZeroTier app is running. IP will be auto-detected.", fyne.TextAlignLeading, fyne.TextStyle{Italic: true}),
	)

	// VPN Mode selector
	currentMode := config.VPNMode
	if currentMode == "" {
		currentMode = "zerotier"
	}

	// Dynamic section that toggles between ZeroTier and L2TP
	dynamicSection := container.NewVBox()
	if currentMode == "l2tp" {
		dynamicSection.Add(l2tpSection)
	} else {
		dynamicSection.Add(ztSection)
	}

	vpnModeSelect.OnChanged = func(selected string) {
		dynamicSection.Objects = nil
		if selected == "L2TP VPN" {
			dynamicSection.Add(l2tpSection)
		} else if selected == "ZeroTier VPN" {
			dynamicSection.Add(ztSection)
		}
		dynamicSection.Refresh()
	}

	btnSave := widget.NewButtonWithIcon("Save Settings", theme.DocumentSaveIcon(), func() {
		config.ServerURL = entryURL.Text
		config.CloudflareToken = entryCF.Text
		config.ApiUsername = entryApiUser.Text
		config.ApiPassword = entryApiPass.Text

		if vpnModeSelect.Selected == "None (Direct/Tunnel Only)" {
			config.VPNMode = "none"
		} else if vpnModeSelect.Selected == "L2TP VPN" {
			config.VPNMode = "l2tp"
		} else {
			config.VPNMode = "zerotier"
		}

		switch engineSelect.Selected {
		case "MediaMTX (Scalable)":
			config.StreamEngine = "mediamtx"
		case "FFmpeg (Transcode/Pro)":
			config.StreamEngine = "ffmpeg"
		default:
			config.StreamEngine = "go2rtc"
		}

		if config.VPNMode == "l2tp" {
			config.L2TPServer = entryL2TPServer.Text
			config.L2TPUser = entryL2TPUser.Text
			config.L2TPPass = entryL2TPPass.Text
			config.L2TPPSK = entryL2TPPSK.Text
		} else {
			config.VPNMode = "none"
		}

		err := saveConfig()
		if err != nil {
			dialog.ShowError(err, myWindow)
		} else {
			dialog.ShowInformation("Success", "Settings saved successfully.", myWindow)
			addLog(fmt.Sprintf("Settings updated. Server: %s, VPN: %s", config.ServerURL, config.VPNMode))
		}
	})
	btnSave.Importance = widget.HighImportance

	content := container.NewBorder(
		nil, container.NewPadded(btnSave), nil, nil,
		container.NewVBox(
			widget.NewLabelWithStyle("RTSP2go Cloud & Network", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
			widget.NewForm(
				widget.NewFormItem("Server URL", entryURL),
				widget.NewFormItem("API Username", entryApiUser),
				widget.NewFormItem("API Password", entryApiPass),
				widget.NewFormItem("Cloudflare Token", entryCF),
				widget.NewFormItem("VPN Mode", vpnModeSelect),
				widget.NewFormItem("Stream Engine", engineSelect),
			),
			dynamicSection,
		),
	)
	return container.NewPadded(content)
}

func refreshCameraList() {
	listContainer.Objects = nil
	instances = nil
	for _, cam := range config.Cameras {
		inst := createTunnelInstance(cam)
		instances = append(instances, inst)
		inst.Card = createCameraCard(inst)
		listContainer.Add(inst.Card)
	}
	listContainer.Refresh()
}

func showAddCameraDialog() {
	entryName := widget.NewEntry()
	entryName.SetPlaceHolder("e.g., Kamera Balkon")
	entryRTSP := widget.NewEntry()
	entryRTSP.SetPlaceHolder("rtsp://user:pass@192.168.1.100:554/stream")

	btnTest := widget.NewButtonWithIcon("Test Local", theme.ViewRefreshIcon(), func() {
		rtspURL := entryRTSP.Text
		if rtspURL == "" {
			dialog.ShowError(fmt.Errorf("Please enter an RTSP URL first."), myWindow)
			return
		}

		addLog(fmt.Sprintf("Testing RTSP connection to: %s", rtspURL))

		go func() {
			err := checkRTSPConnection(rtspURL)
			if err != nil {
				addLog(fmt.Sprintf("RTSP Check Failed: %v", err))
				dialog.ShowError(fmt.Errorf("RTSP Connection Failed:\n%v", err), myWindow) // Using dialog.ShowError for simplicity but keep in mind it runs async so could occasionally block if heavy interaction happens.
			} else {
				addLog("RTSP Check Success: Camera is online and responding.")
				dialog.ShowInformation("Connection Success", "Camera is online and responding to RTSP requests!", myWindow)
			}
		}()
	})

	items := []*widget.FormItem{
		widget.NewFormItem("Name", entryName),
		widget.NewFormItem("Local RTSP", container.NewBorder(nil, nil, nil, btnTest, entryRTSP)),
	}
	form := widget.NewForm(items...)

	content := container.NewVBox(
		widget.NewLabel("Enter camera details below:"),
		form,
		widget.NewLabelWithStyle("* Test Local verifies camera connectivity without opening a video player.", fyne.TextAlignLeading, fyne.TextStyle{Italic: true}),
	)

	editDialog := dialog.NewCustomConfirm("Add Camera Stream", "Save", "Cancel",
		content, func(save bool) {
			if save {
				if entryName.Text == "" || entryRTSP.Text == "" {
					dialog.ShowError(fmt.Errorf("Name and RTSP URL are required"), myWindow)
					return
				}
				newCam := CameraConfig{
					Name:      entryName.Text,
					LocalRTSP: entryRTSP.Text,
				}
				config.Cameras = append(config.Cameras, newCam)
				saveConfig()
				refreshCameraList()
				addLog(fmt.Sprintf("Camera added: %s", newCam.Name))
			}
		}, myWindow)

	editDialog.Resize(fyne.NewSize(600, 200))
	editDialog.Show()
}

func importCSV() {
	d := dialog.NewFileOpen(func(r fyne.URIReadCloser, err error) {
		if err != nil {
			dialog.ShowError(err, myWindow)
			return
		}
		if r == nil {
			return // cancelled
		}
		defer r.Close()

		addLog("Importing cameras from CSV...")
		reader := csv.NewReader(r)
		records, err := reader.ReadAll()
		if err != nil {
			dialog.ShowError(fmt.Errorf("Failed to parse CSV: %v", err), myWindow)
			return
		}

		addedCount := 0
		for i, row := range records {
			// Skip header row if matches "Name"
			if i == 0 && len(row) > 0 && strings.Contains(strings.ToLower(row[0]), "name") {
				continue
			}
			if len(row) >= 2 {
				name := strings.TrimSpace(row[0])
				rtsp := strings.TrimSpace(row[1])
				if name != "" && rtsp != "" {
					config.Cameras = append(config.Cameras, CameraConfig{
						Name:      name,
						LocalRTSP: rtsp,
					})
					addedCount++
				}
			}
		}

		if addedCount > 0 {
			saveConfig()
			refreshCameraList()
			dialog.ShowInformation("Import Successful", fmt.Sprintf("Imported %d cameras from CSV file.", addedCount), myWindow)
			addLog(fmt.Sprintf("Successfully imported %d cameras from CSV.", addedCount))
		} else {
			dialog.ShowInformation("No Import", "No valid camera rows found in the CSV. Make sure format is: Name,LocalRTSP", myWindow)
		}
	}, myWindow)

	// Enlarge the file manager dialog (wider & taller)
	d.Resize(fyne.NewSize(800, 600))
	d.Show()
}

func deleteCamera(inst *TunnelInstance) {
	dialog.ShowConfirm("Delete Camera", fmt.Sprintf("Are you sure you want to delete '%s'?", inst.Camera.Name), func(b bool) {
		if b {
			// Run in goroutine: Stop() closes SSH connection which can block
			go func() {
				inst.Stop()

				// Auto Deregister from backend API
				deregisterFromBackend(inst.Camera.Name)

				// Remove from config slice
				for i, c := range config.Cameras {
					if c.Name == inst.Camera.Name {
						config.Cameras = append(config.Cameras[:i], config.Cameras[i+1:]...)
						break
					}
				}
				saveConfig()
				refreshCameraList()
				addLog(fmt.Sprintf("Deleted camera: %s", inst.Camera.Name))
			}()
		}
	}, myWindow)
}

func createTunnelInstance(cam CameraConfig) *TunnelInstance {
	dot := canvas.NewCircle(theme.Color(theme.ColorNameDisabled))
	dot.Resize(fyne.NewSize(12, 12))

	return &TunnelInstance{
		Camera:     cam,
		Status:     "Stopped",
		StatusDot:  dot,
		StatusText: widget.NewLabel("Stopped"),
		StopChan:   make(chan bool),
	}
}

func createCameraCard(inst *TunnelInstance) fyne.CanvasObject {
	title := widget.NewLabelWithStyle(inst.Camera.Name, fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	details := widget.NewLabel(fmt.Sprintf("Target: %s", inst.Camera.LocalRTSP))
	details.Wrapping = fyne.TextTruncate

	statusBox := container.NewHBox(
		container.NewCenter(inst.StatusDot),
		inst.StatusText,
	)

	btnToggle := widget.NewButtonWithIcon("", theme.MediaPlayIcon(), nil)
	inst.ToggleBtn = btnToggle
	btnToggle.OnTapped = func() {
		inst.mu.Lock()
		running := inst.Running
		inst.mu.Unlock()

		if !running {
			go inst.Start()
			btnToggle.SetIcon(theme.MediaStopIcon())
		} else {
			go inst.Stop()
			btnToggle.SetIcon(theme.MediaPlayIcon())
		}
	}

	btnEdit := widget.NewButtonWithIcon("", theme.DocumentCreateIcon(), func() {
		showEditCameraDialog(inst)
	})

	btnDelete := widget.NewButtonWithIcon("", theme.DeleteIcon(), func() {
		deleteCamera(inst)
	})
	btnDelete.Importance = widget.DangerImportance

	controls := container.NewHBox(btnToggle, btnEdit, btnDelete)

	return widget.NewCard("", "", container.NewBorder(
		nil, nil, nil, controls,
		container.NewVBox(title, details, statusBox),
	))
}

func showEditCameraDialog(inst *TunnelInstance) {
	entryName := widget.NewEntry()
	entryName.SetText(inst.Camera.Name)

	entryRTSP := widget.NewEntry()
	entryRTSP.SetText(inst.Camera.LocalRTSP)

	btnTest := widget.NewButtonWithIcon("Test Local", theme.ViewRefreshIcon(), func() {
		if err := checkRTSPConnection(entryRTSP.Text); err != nil {
			dialog.ShowError(fmt.Errorf("⚠️ RTSP Test Failed: %v", err), myWindow)
		} else {
			dialog.ShowInformation("RTSP Test", "✅ Connection successful!", myWindow)
		}
	})

	items := []*widget.FormItem{
		widget.NewFormItem("Name", entryName),
		widget.NewFormItem("Local RTSP", container.NewBorder(nil, nil, nil, btnTest, entryRTSP)),
	}
	form := widget.NewForm(items...)

	var editDialog dialog.Dialog
	content := container.NewVBox(
		widget.NewLabel("Edit camera details:"),
		form,
	)

	oldName := inst.Camera.Name

	editDialog = dialog.NewCustomConfirm("Edit Camera", "Save Changes", "Cancel", content, func(b bool) {
		if !b {
			return
		}
		if entryName.Text == "" || entryRTSP.Text == "" {
			dialog.ShowError(fmt.Errorf("Please fill all fields correctly"), myWindow)
			return
		}

		// Stop the tunnel if running
		if inst.Running {
			go inst.Stop()
		}

		// Update the config
		for i, cam := range config.Cameras {
			if cam.Name == oldName {
				config.Cameras[i].Name = entryName.Text
				config.Cameras[i].LocalRTSP = entryRTSP.Text
				break
			}
		}
		saveConfig()

		// Update VPS backend via PUT (originalName is the old name)
		go func() {
			apiURL := getServerAPIURL()
			if apiURL == "" {
				return
			}
			payload := map[string]string{
				"name":         entryName.Text,
				"url":          entryRTSP.Text,
				"originalName": oldName,
				"backend":      "go2rtc",
			}
			jsonData, _ := json.Marshal(payload)
			req, err := http.NewRequest(http.MethodPut, apiURL, bytes.NewBuffer(jsonData))
			if err != nil {
				addLog(fmt.Sprintf("[%s] Edit sync Error: %v", entryName.Text, err))
				return
			}
			req.Header.Set("Content-Type", "application/json")
			if config.ApiUsername != "" && config.ApiPassword != "" {
				req.SetBasicAuth(config.ApiUsername, config.ApiPassword)
			}
			client := &http.Client{Timeout: 5 * time.Second}
			resp, err := client.Do(req)
			if err != nil {
				addLog(fmt.Sprintf("[%s] Edit sync to VPS failed: %v", entryName.Text, err))
				return
			}
			defer resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				addLog(fmt.Sprintf("[%s] ✅ Camera updated on VPS backend.", entryName.Text))
			} else {
				body, _ := ioutil.ReadAll(resp.Body)
				addLog(fmt.Sprintf("[%s] ⚠️ VPS update returned %d: %s", entryName.Text, resp.StatusCode, string(body)))
			}
		}()

		refreshCameraList()
		addLog(fmt.Sprintf("Camera '%s' updated (was: '%s')", entryName.Text, oldName))
	}, myWindow)

	editDialog.Resize(fyne.NewSize(600, 280))
	editDialog.Show()
}

func connectL2TP() error {
	if config.L2TPServer == "" || config.L2TPUser == "" || config.L2TPPass == "" {
		return fmt.Errorf("L2TP credentials not configured. Go to Settings.")
	}

	addLog("L2TP: Creating VPN phonebook entry...")

	// Create/update the L2TP VPN entry using PowerShell
	psScript := fmt.Sprintf(`
$vpnName = 'RTSP2go-VPN'
Remove-VpnConnection -Name $vpnName -Force -ErrorAction SilentlyContinue
Add-VpnConnection -Name $vpnName -ServerAddress '%s' -TunnelType L2tp -L2tpPsk '%s' -AuthenticationMethod MSChapv2 -EncryptionLevel Optional -Force
`, config.L2TPServer, config.L2TPPSK)

	cmd := exec.Command("powershell", "-Command", psScript)
	out, err := cmd.CombinedOutput()
	if err != nil {
		addLog(fmt.Sprintf("L2TP: Failed to create VPN entry: %v — %s", err, string(out)))
		return fmt.Errorf("failed to create L2TP VPN entry: %v", err)
	}

	addLog("L2TP: Connecting...")
	cmd = exec.Command("rasdial", "RTSP2go-VPN", config.L2TPUser, config.L2TPPass)
	out, err = cmd.CombinedOutput()
	if err != nil {
		addLog(fmt.Sprintf("L2TP: Connection failed: %v — %s", err, string(out)))
		return fmt.Errorf("L2TP connection failed: %v", err)
	}

	addLog("L2TP: ✅ Connected successfully!")
	return nil
}

func disconnectL2TP() {
	addLog("L2TP: Disconnecting...")
	cmd := exec.Command("rasdial", "RTSP2go-VPN", "/disconnect")
	out, err := cmd.CombinedOutput()
	if err != nil {
		addLog(fmt.Sprintf("L2TP: Disconnect warning: %v — %s", err, string(out)))
	}
	addLog("L2TP: Disconnected.")
}

func startCloudflareTunnel() error {
	if config.CloudflareToken == "" {
		return nil
	}
	addLog("Cloudflare: Starting tunnel...")
	// Try to execute cloudflared
	cmd := exec.Command("cloudflared", "tunnel", "--no-autoupdate", "run", "--token", config.CloudflareToken)
	err := cmd.Start()
	if err != nil {
		addLog(fmt.Sprintf("Cloudflare: ❌ Failed to start cloudflared: %v. Please ensure it is installed.", err))
		return err
	}
	addLog("Cloudflare: ✅ Tunnel process started in background.")
	return nil
}

func stopCloudflareTunnel() {
	addLog("Cloudflare: Stopping tunnel...")
	// On Windows we usually taskkill or let OS handle it, but for a simple implementation:
	if os.Getenv("OS") == "Windows_NT" {
		exec.Command("taskkill", "/F", "/IM", "cloudflared.exe").Run()
	} else {
		exec.Command("pkill", "cloudflared").Run()
	}
}

func getServerAPIURL() string {
	if config.ServerURL != "" {
		u := config.ServerURL
		if !strings.HasPrefix(u, "http") {
			u = "http://" + u
		}
		if !strings.Contains(u, "/api/streams") {
			u = strings.TrimSuffix(u, "/") + "/api/streams"
		}
		return u
	}
	return ""
}

func startAll() {
	if config.ServerURL == "" {
		dialog.ShowError(fmt.Errorf("Please set Server URL in Settings first."), myWindow)
		return
	}

	vpnMode := config.VPNMode
	if vpnMode == "" {
		vpnMode = "zerotier"
	}

	go func() {
		if vpnMode == "l2tp" {
			addLog("Global: Connecting L2TP VPN...")
			err := connectL2TP()
			if err != nil {
				dialog.ShowError(fmt.Errorf("L2TP Connection Failed:\n%v", err), myWindow)
				addLog(fmt.Sprintf("Global ❌ L2TP connection failed: %v", err))
				return
			}
			// Wait for interface to get an IP
			time.Sleep(3 * time.Second)
		} else if vpnMode == "zerotier" {
			addLog("Global: Validating ZeroTier VPN Connection...")
		}

		// Cloudflare Tunnel
		if config.CloudflareToken != "" {
			startCloudflareTunnel()
		}

		ip := getVpnIP()
		if ip == "" && vpnMode != "none" {
			if vpnMode == "l2tp" {
				dialog.ShowError(fmt.Errorf("L2TP VPN IP not found after connecting.\nCheck your L2TP server and credentials."), myWindow)
				addLog("Global ❌ L2TP IP detection failed.")
			} else {
				dialog.ShowError(fmt.Errorf("ZeroTier IPv4 address not found.\nEnsure the ZeroTier app is running and connected."), myWindow)
				addLog("Global ❌ ZeroTier connection missing. Cannot start streams.")
			}
			return
		}

		if ip != "" {
			addLog(fmt.Sprintf("Global ✅ VPN Connected — Mode: %s (IP: %s)", strings.ToUpper(vpnMode), ip))
		}

		addLog("Global: Registering all cameras to backend...")
		for _, inst := range instances {
			go inst.Start()
		}
	}()
}

func stopAll() {
	go func() {
		addLog("Global: Stopping all streams...")

		// Stop cameras first (deregisters them from VPS via the VPN tunnel before we kill it)
		var wg sync.WaitGroup
		for _, inst := range instances {
			wg.Add(1)
			go func(i *TunnelInstance) {
				defer wg.Done()
				i.Stop()
			}(inst)
		}
		wg.Wait()

		// If L2TP mode, disconnect the VPN after stopping all streams
		vpnMode := config.VPNMode
		if vpnMode == "l2tp" {
			disconnectL2TP()
		}
		stopCloudflareTunnel()

		addLog("Global ✅ All Processes Stopped.")
	}()
}

func (inst *TunnelInstance) Start() {
	inst.mu.Lock()
	if inst.Running {
		inst.mu.Unlock()
		return
	}
	inst.Running = true
	inst.mu.Unlock()

	inst.updateStatus("Connecting", theme.ColorNameWarning)
	addLog(fmt.Sprintf("[%s] Preparing stream...", inst.Camera.Name))

	// Under ZeroTier SDN, starting a camera simply means registering it to the VPS
	// assuming the VPN is already connected globally via startAll()
	go registerToBackend(inst)

	inst.updateStatus("Online", theme.ColorNameSuccess)

	// Wait for stop signal
	<-inst.StopChan
}

func (inst *TunnelInstance) Stop() {
	inst.mu.Lock()
	defer inst.mu.Unlock()

	if !inst.Running {
		return
	}

	inst.Running = false

	if inst.proxyLn != nil {
		inst.proxyLn.Close()
		inst.proxyLn = nil
	}

	close(inst.StopChan)
	inst.StopChan = make(chan bool)
	go deregisterFromBackend(inst.Camera.Name)
	inst.updateStatus("Stopped", theme.ColorNameDisabled)
	if inst.ToggleBtn != nil {
		inst.ToggleBtn.SetIcon(theme.MediaPlayIcon())
	}
	addLog(fmt.Sprintf("[%s] Manual stop requested.", inst.Camera.Name))
}

func (inst *TunnelInstance) updateStatus(text string, colorName fyne.ThemeColorName) {
	inst.Status = text
	inst.StatusText.SetText(text)
	inst.StatusDot.FillColor = theme.Color(colorName)
	inst.StatusDot.Refresh()
}

func addLog(msg string) {
	logMu.Lock()
	defer logMu.Unlock()
	timestamp := time.Now().Format("15:04:05")
	logData = append([]string{fmt.Sprintf("[%s] %s", timestamp, msg)}, logData...)
	if len(logData) > 50 {
		logData = logData[:50]
	}
	if globalLogs != nil {
		globalLogs.SetText(strings.Join(logData, "\n"))
	}
}

func loadConfig() error {
	data, err := ioutil.ReadFile(configPath)
	if err != nil {
		return err
	}
	var cfg Config
	err = json.Unmarshal(data, &cfg)
	if err == nil {
		config = &cfg
	}
	return err
}

func saveConfig() error {
	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return err
	}
	return ioutil.WriteFile(configPath, data, 0644)
}

func cleanup() {
	stopAll()
}

func extractHostPort(uri string) string {
	start := 0
	for i := 0; i < len(uri); i++ {
		if uri[i] == '@' {
			start = i + 1
		}
	}
	if start == 0 {
		for i := 0; i < len(uri)-7; i++ {
			if uri[i:i+7] == "rtsp://" {
				start = i + 7
			}
		}
	}
	end := len(uri)
	for i := start; i < len(uri); i++ {
		if uri[i] == '/' {
			end = i
			break
		}
	}
	hostPort := uri[start:end]

	hasPort := false
	for i := 0; i < len(hostPort); i++ {
		if hostPort[i] == ':' {
			hasPort = true
			break
		}
	}
	if !hasPort {
		hostPort += ":554"
	}
	return hostPort
}

func checkRTSPConnection(uri string) error {
	hostPort := extractHostPort(uri)
	conn, err := net.DialTimeout("tcp", hostPort, 3*time.Second)
	if err != nil {
		return fmt.Errorf("TCP Dial failed: %w", err)
	}
	defer conn.Close()

	req := fmt.Sprintf("OPTIONS %s RTSP/1.0\r\nCSeq: 1\r\nUser-Agent: WebTR-Gateway\r\n\r\n", uri)
	_, err = conn.Write([]byte(req))
	if err != nil {
		return fmt.Errorf("failed to send RTSP OPTIONS: %w", err)
	}

	err = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	if err != nil {
		return err
	}

	buf := make([]byte, 1024)
	n, err := conn.Read(buf)
	if err != nil {
		return fmt.Errorf("failed to read RTSP response: %w", err)
	}

	response := string(buf[:n])
	if !containsStr(response, "RTSP/1.0 200 OK") && !containsStr(response, "RTSP/1.0 401 Unauthorized") {
		limit := 100
		if len(response) < limit {
			limit = len(response)
		}
		return fmt.Errorf("unexpected Server response:\n%s", response[:limit])
	}
	return nil
}

func containsStr(s, substr string) bool {
	if len(substr) == 0 {
		return true
	}
	if len(substr) > len(s) {
		return false
	}
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// Zero-Config Auto Registration Helpers

func getVpnIP() string {
	vpnMode := config.VPNMode
	if vpnMode == "" {
		vpnMode = "zerotier"
	}

	var psCommand string
	if vpnMode == "l2tp" {
		// L2TP creates a PPP adapter; detect its IP
		psCommand = "(Get-NetIPAddress -InterfaceAlias '*PPP*','*VPN*','*L2TP*','*WebTR*','*RAS*' -AddressFamily IPv4 -ErrorAction SilentlyContinue).IPAddress"
	} else {
		// ZeroTier interface
		psCommand = "(Get-NetIPAddress -InterfaceAlias 'ZeroTier*' -AddressFamily IPv4 -ErrorAction SilentlyContinue).IPAddress"
	}

	cmd := exec.Command("powershell", "-Command", psCommand)
	out, err := cmd.Output()
	if err == nil {
		ip := strings.TrimSpace(string(out))
		if ip != "" {
			ips := strings.Split(ip, "\n")
			return strings.TrimSpace(ips[0])
		}
	}
	return ""
}

func isPrivateIP(hostPort string) bool {
	host, _, err := net.SplitHostPort(hostPort)
	if err != nil {
		host = hostPort
	}
	ip := net.ParseIP(host)
	if ip == nil {
		// If it's a hostname (not IP), assume it might be local — use tunnel
		return true
	}
	privateRanges := []string{
		"10.0.0.0/8",
		"172.16.0.0/12",
		"192.168.0.0/16",
		"127.0.0.0/8",
		"169.254.0.0/16",
	}
	for _, cidr := range privateRanges {
		_, network, _ := net.ParseCIDR(cidr)
		if network.Contains(ip) {
			return true
		}
	}
	return false
}

// replaceHostPortInRTSP replaces the host:port in an RTSP URL while keeping
// the scheme, credentials (user:pass@), and path intact.
// e.g. rtsp://user:pass@192.168.1.1:554/path → rtsp://user:pass@127.0.0.1:PORT/path
func replaceHostPortInRTSP(originalURL, newHostPort string) string {
	// Find the scheme end (after "rtsp://")
	schemeEnd := 0
	for i := 0; i < len(originalURL)-6; i++ {
		if originalURL[i:i+7] == "rtsp://" {
			schemeEnd = i + 7
			break
		}
	}
	if schemeEnd == 0 {
		return fmt.Sprintf("rtsp://%s", newHostPort)
	}

	prefix := originalURL[:schemeEnd] // "rtsp://"

	afterScheme := originalURL[schemeEnd:]

	// Find the @ symbol (end of credentials)
	atIdx := -1
	for i, c := range afterScheme {
		if c == '@' {
			atIdx = i
		}
	}

	var credentials string
	var hostAndRest string
	if atIdx >= 0 {
		credentials = afterScheme[:atIdx+1] // "user:pass@"
		hostAndRest = afterScheme[atIdx+1:] // "192.168.1.1:554/path"
	} else {
		credentials = ""
		hostAndRest = afterScheme
	}

	// Find the path (everything after the first '/')
	pathIdx := -1
	for i, c := range hostAndRest {
		if c == '/' {
			pathIdx = i
			break
		}
	}

	var path string
	if pathIdx >= 0 {
		path = hostAndRest[pathIdx:] // "/live1s2.sdp"
	} else {
		path = ""
	}

	return prefix + credentials + newHostPort + path
}

// startTCPProxy creates a transparent port-forward from 0.0.0.0 (dynamic port) to the target NVR IP.
func startTCPProxy(inst *TunnelInstance, targetHostPort string) error {
	listener, err := net.Listen("tcp", "0.0.0.0:0")
	if err != nil {
		return err
	}
	inst.mu.Lock()
	inst.proxyLn = listener
	inst.proxyPort = listener.Addr().(*net.TCPAddr).Port
	inst.mu.Unlock()

	go func() {
		for {
			client, err := listener.Accept()
			if err != nil {
				// Listener closed
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				remote, err := net.DialTimeout("tcp", targetHostPort, 5*time.Second)
				if err != nil {
					addLog(fmt.Sprintf("[%s] Proxy Dial Error: %v", inst.Camera.Name, err))
					return
				}
				defer remote.Close()

				errc := make(chan error, 2)
				go func() { _, err := io.Copy(remote, c); errc <- err }()
				go func() { _, err := io.Copy(c, remote); errc <- err }()
				<-errc
			}(client)
		}
	}()
	return nil
}

func registerToBackend(inst *TunnelInstance) {
	camName := inst.Camera.Name
	// Give the tunnel a brief moment to stabilize
	time.Sleep(2 * time.Second)

	// Ensure we have a backend configured
	apiURL := getServerAPIURL()
	if apiURL == "" {
		addLog(fmt.Sprintf("[%s] Skip Auto-Register: Server URL not set.", camName))
		return
	}
	var originalRTSP string
	for _, cam := range config.Cameras {
		if cam.Name == camName {
			originalRTSP = cam.LocalRTSP
			break
		}
	}

	var rtspSource string
	hostPort := extractHostPort(originalRTSP)
	if isPrivateIP(hostPort) {
		vpnIP := getVpnIP()
		if vpnIP == "" {
			addLog(fmt.Sprintf("[%s] ❌ Cannot register: VPN IP not found. Is the VPN connected?", camName))
			return
		}

		// Start the TCP proxy for this specific camera
		err := startTCPProxy(inst, hostPort)
		if err != nil {
			addLog(fmt.Sprintf("[%s] ❌ Failed to start local proxy: %v", camName, err))
			return
		}

		addLog(fmt.Sprintf("[%s] Local proxy started on port %d -> %s", camName, inst.proxyPort, hostPort))

		// Replace the RTSP URL so it connects to our local proxy via ZeroTier interface
		proxyHostPort := fmt.Sprintf("%s:%d", vpnIP, inst.proxyPort)
		rtspSource = replaceHostPortInRTSP(originalRTSP, proxyHostPort)

		addLog(fmt.Sprintf("[%s] Camera is LOCAL — proxying via VPN IP: %s", camName, rtspSource))
	} else {
		// Camera has a public IP — go2rtc can reach it directly!
		rtspSource = originalRTSP
		addLog(fmt.Sprintf("[%s] Camera is PUBLIC — registering direct RTSP URL", camName))
	}

	addLog(fmt.Sprintf("[%s] Auto-Registering on VPS backend: %s", camName, apiURL))

	// Use POST to ADD a new stream
	payload := map[string]interface{}{
		"name":    camName,
		"url":     rtspSource,
		"backend": config.StreamEngine, // go2rtc, mediamtx, or ffmpeg
		"lat":     0,
		"lng":     0,
	}

	jsonData, err := json.Marshal(payload)
	if err != nil {
		addLog(fmt.Sprintf("[%s] Register Error: JSON marshal failed: %v", camName, err))
		return
	}

	req, err := http.NewRequest(http.MethodPost, apiURL, bytes.NewBuffer(jsonData))
	if err != nil {
		addLog(fmt.Sprintf("[%s] Register Error: Request creation failed: %v", camName, err))
		return
	}
	req.Header.Set("Content-Type", "application/json")
	if config.ApiUsername != "" && config.ApiPassword != "" {
		req.SetBasicAuth(config.ApiUsername, config.ApiPassword)
	}

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		addLog(fmt.Sprintf("[%s] Auto-Register Failed: Cannot reach VPS backend via HTTP. (%v)", camName, err))
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusCreated {
		addLog(fmt.Sprintf("[%s] Successfully registered to web-tr backend!", camName))
	} else {
		// Read body for error context
		bodyBytes, _ := ioutil.ReadAll(resp.Body)
		addLog(fmt.Sprintf("[%s] Warning: Backend returned status %d: %s", camName, resp.StatusCode, string(bodyBytes)))
	}
}

func deregisterFromBackend(camName string) {
	apiURL := getServerAPIURL()
	if apiURL == "" {
		return
	}
	// Append query param to the base API URL
	fullURL := fmt.Sprintf("%s?name=%s", apiURL, url.QueryEscape(camName))
	addLog(fmt.Sprintf("[%s] Sending DELETE to VPS: %s", camName, fullURL))

	req, err := http.NewRequest(http.MethodDelete, fullURL, nil)
	if err != nil {
		addLog(fmt.Sprintf("[%s] Auto-Deregister Error: %v", camName, err))
		return
	}
	if config.ApiUsername != "" && config.ApiPassword != "" {
		req.SetBasicAuth(config.ApiUsername, config.ApiPassword)
	}

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		addLog(fmt.Sprintf("[%s] Auto-Deregister Failed: %v", camName, err))
		return
	}
	defer resp.Body.Close()

	bodyBytes, _ := ioutil.ReadAll(resp.Body)
	if resp.StatusCode == http.StatusOK {
		addLog(fmt.Sprintf("[%s] ✅ Successfully deregistered from VPS backend.", camName))
	} else {
		addLog(fmt.Sprintf("[%s] ❌ Deregister failed — Status: %d, Body: %s", camName, resp.StatusCode, string(bodyBytes)))
	}
}

// VPS Diagnostics
func diagnoseVPS() {
	if config == nil || config.ServerURL == "" {
		dialog.ShowError(fmt.Errorf("Server URL is not configured. Go to Settings first."), myWindow)
		return
	}

	addLog("--- VPS Diagnostics Starting ---")

	go func() {
		results := ""

		// 1. Check RTSP2go API connection
		addLog("[Diagnose] Testing RTSP2go API connection...")
		apiURL := getServerAPIURL()
		if apiURL == "" {
			results += "❌ RTSP2go API: ERROR (Server URL not configured)\n\n"
		} else {
			client := &http.Client{Timeout: 5 * time.Second}
			resp, err := client.Get(apiURL)
			if err != nil {
				results += "❌ RTSP2go API: UNREACHABLE\n    " + err.Error() + "\n\n"
				addLog("[Diagnose] RTSP2go API: UNREACHABLE — Auto-Register will fail!")
			} else {
				body, _ := io.ReadAll(resp.Body)
				resp.Body.Close()
				results += fmt.Sprintf("✅ RTSP2go API: OK (Code: %d)\n    Cameras Registered: %s\n\n", resp.StatusCode, string(body))
				addLog(fmt.Sprintf("[Diagnose] RTSP2go API: OK — Found streams: %s", string(body)))
			}
		}

		// 2. Show registered cameras from the local config
		results += fmt.Sprintf("📋 Local Config: %d cameras configured\n", len(config.Cameras))
		for _, cam := range config.Cameras {
			results += fmt.Sprintf("    • %s\n", cam.Name)
		}

		addLog("--- VPS Diagnostics Complete ---")

		dialog.ShowInformation("VPS Diagnostics Result", results, myWindow)
	}()
}

func openDashboard() {
	if config == nil || config.ServerURL == "" {
		dialog.ShowError(fmt.Errorf("Server URL is not configured. Go to Settings first."), myWindow)
		return
	}

	dashURL := config.ServerURL
	if !strings.HasPrefix(dashURL, "http") {
		dashURL = "http://" + dashURL
	}
	// Try to get base domain (remove /api/streams if present)
	dashURL = strings.Split(dashURL, "/api/")[0]
	dashURL = strings.TrimSuffix(dashURL, "/")

	addLog(fmt.Sprintf("Opening Dashboard: %s", dashURL))

	// Open in default browser
	exec.Command("rundll32", "url.dll,FileProtocolHandler", dashURL).Start()
}
func hasRunningTunnel() bool {
	for _, inst := range instances {
		inst.mu.Lock()
		r := inst.Running
		inst.mu.Unlock()
		if r {
			return true
		}
	}
	return false
}

func startMonitoring() {
	go func() {
		var lastRecv, lastSent uint64
		first := true
		for {
			// Only show real stats when at least one tunnel is running
			if !hasRunningTunnel() {
				idleText := "💻 CPU: 0% | 🔼 Up: 0 B/s | 🔽 Down: 0 B/s"
				if lblMonitor != nil {
					lblMonitor.SetText(idleText)
				}
				if trayCPUItem != nil {
					trayCPUItem.Label = "💻 CPU: -- idle --"
					trayUpItem.Label = "🔼 Up: -- idle --"
					trayDownItem.Label = "🔽 Down: -- idle --"
					if trayMenu != nil {
						trayMenu.Refresh()
					}
				}
				// Reset counters so next active reading is fresh
				first = true
				time.Sleep(2 * time.Second)
				continue
			}

			c, _ := cpu.Percent(0, false)
			n, _ := psnet.IOCounters(false)

			cpuStr := "0.0"
			if len(c) > 0 {
				cpuStr = fmt.Sprintf("%.1f", c[0])
			}

			upStr, downStr := "0 B/s", "0 B/s"
			if len(n) > 0 {
				stat := n[0]
				if !first {
					upBytes := stat.BytesSent - lastSent
					downBytes := stat.BytesRecv - lastRecv
					upStr = formatBytes(upBytes)
					downStr = formatBytes(downBytes)
				}
				lastSent = stat.BytesSent
				lastRecv = stat.BytesRecv
				first = false
			}

			text := fmt.Sprintf("💻 CPU: %s%% | 🔼 Up: %s | 🔽 Down: %s", cpuStr, upStr, downStr)
			if lblMonitor != nil {
				lblMonitor.SetText(text)
			}

			if trayCPUItem != nil {
				trayCPUItem.Label = fmt.Sprintf("💻 CPU: %s%%", cpuStr)
				trayUpItem.Label = fmt.Sprintf("🔼 Up: %s", upStr)
				trayDownItem.Label = fmt.Sprintf("🔽 Down: %s", downStr)
				if trayMenu != nil {
					trayMenu.Refresh()
				}
			}

			time.Sleep(1 * time.Second)
		}
	}()
}

func formatBytes(b uint64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B/s", b)
	}
	if b < unit*unit {
		return fmt.Sprintf("%.1f KB/s", float64(b)/unit)
	}
	if b < unit*unit*unit {
		return fmt.Sprintf("%.1f MB/s", float64(b)/(unit*unit))
	}
	return fmt.Sprintf("%.1f GB/s", float64(b)/(unit*unit*unit))
}
