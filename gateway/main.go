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
	"path/filepath"
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
	ApiUsername     string         `json:"api_username"`
	ApiPassword     string         `json:"api_password"`
	VPNMode          string         `json:"vpn_mode"`
	StreamEngine    string         `json:"stream_engine"` // "go2rtc", "mediamtx", "ffmpeg"
	L2TPServer      string         `json:"l2tp_server"`
	L2TPUser        string         `json:"l2tp_user"`
	L2TPPass        string         `json:"l2tp_pass"`
	L2TPPSK         string         `json:"l2tp_psk"`
	WireGuardConfig string         `json:"wireguard_config"`
	KeepCameraOnStop bool          `json:"keep_camera_on_stop"`
	Cameras         []CameraConfig `json:"cameras"`
}

type GatewaySettings struct {
	Username     string `json:"username"`
	Plan         string `json:"plan"`
	MaxCameras   int    `json:"max_cameras"`
	LimitMinutes int    `json:"limit_minutes"`
	IsTrial      bool   `json:"is_trial"`
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
	proxyLn     net.Listener
	proxyPort   int
	PublicURL   string
	PublicLabel *widget.Entry // Use Entry for easy selection/copy
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
	instances      []*TunnelInstance
	config         *Config
	serverSettings GatewaySettings
	configPath     = "config.json"
	globalLogs     *widget.Entry
	logData        []string
	logMu          sync.Mutex
	myApp          fyne.App
	myWindow       fyne.Window
	listContainer  *fyne.Container
	lblMonitor     *widget.Label
	trayMenu       *fyne.Menu
	trayCPUItem    *fyne.MenuItem
	trayUpItem     *fyne.MenuItem
	trayDownItem   *fyne.MenuItem
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

	myWindow.SetCloseIntercept(func() {
		myWindow.Hide()
		addLog("App minimized to system tray. Tunnels are still actively managed.")
	})

	// Initial sync of subscription limits
	go fetchGatewaySettings()

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

	entryApiUser := widget.NewEntry()
	entryApiUser.SetPlaceHolder("Admin / Gateway User")
	entryApiUser.SetText(config.ApiUsername)

	entryApiPass := widget.NewPasswordEntry()
	entryApiPass.SetPlaceHolder("Password")
	entryApiPass.SetText(config.ApiPassword)

	vpnModes := []string{"WireGuard VPN", "ZeroTier VPN", "L2TP VPN", "None (Direct/Tunnel Only)"}
	vpnModeSelect := widget.NewSelect(vpnModes, nil)
	if config.VPNMode == "none" {
		vpnModeSelect.SetSelected("None (Direct/Tunnel Only)")
	} else if config.VPNMode == "l2tp" {
		vpnModeSelect.SetSelected("L2TP VPN")
	} else if config.VPNMode == "wireguard" {
		vpnModeSelect.SetSelected("WireGuard VPN")
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

	entryWG := widget.NewMultiLineEntry()
	entryWG.SetPlaceHolder("[Interface]\nPrivateKey=...\nAddress=...\n\n[Peer]\nPublicKey=...\nEndpoint=...")
	entryWG.SetText(config.WireGuardConfig)
	wgSection := container.NewVBox(
		widget.NewSeparator(),
		widget.NewLabelWithStyle("WireGuard Configuration", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		widget.NewLabelWithStyle("Paste your entire wg0.conf text below:", fyne.TextAlignLeading, fyne.TextStyle{Italic: true}),
		container.NewGridWrap(fyne.NewSize(450, 150), entryWG),
	)

	checkKeepCamera := widget.NewCheck("Keep camera on server when stopped (manual stop)", nil)
	checkKeepCamera.SetChecked(config.KeepCameraOnStop)

	// VPN Mode selector
	currentMode := config.VPNMode
	if currentMode == "" {
		currentMode = "zerotier"
	}

	// Dynamic section that toggles between ZeroTier, WireGuard, and L2TP
	dynamicSection := container.NewVBox()
	if currentMode == "l2tp" {
		dynamicSection.Add(l2tpSection)
	} else if currentMode == "wireguard" {
		dynamicSection.Add(wgSection)
	} else {
		dynamicSection.Add(ztSection)
	}

	vpnModeSelect.OnChanged = func(selected string) {
		dynamicSection.Objects = nil
		if selected == "L2TP VPN" {
			dynamicSection.Add(l2tpSection)
		} else if selected == "WireGuard VPN" {
			dynamicSection.Add(wgSection)
		} else if selected == "ZeroTier VPN" {
			dynamicSection.Add(ztSection)
		}
		dynamicSection.Refresh()
	}

	btnSave := widget.NewButtonWithIcon("Save Settings", theme.DocumentSaveIcon(), func() {
		config.ServerURL = entryURL.Text
		config.ApiUsername = entryApiUser.Text
		config.ApiPassword = entryApiPass.Text
		config.KeepCameraOnStop = checkKeepCamera.Checked

		if vpnModeSelect.Selected == "None (Direct/Tunnel Only)" {
			config.VPNMode = "none"
		} else if vpnModeSelect.Selected == "L2TP VPN" {
			config.VPNMode = "l2tp"
		} else if vpnModeSelect.Selected == "WireGuard VPN" {
			config.VPNMode = "wireguard"
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

		config.WireGuardConfig = entryWG.Text

		if config.VPNMode == "l2tp" {
			config.L2TPServer = entryL2TPServer.Text
			config.L2TPUser = entryL2TPUser.Text
			config.L2TPPass = entryL2TPPass.Text
			config.L2TPPSK = entryL2TPPSK.Text
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
				widget.NewFormItem("VPN Mode", vpnModeSelect),
				widget.NewFormItem("Stream Engine", engineSelect),
				widget.NewFormItem("", checkKeepCamera),
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
	if serverSettings.MaxCameras > 0 && len(config.Cameras) >= serverSettings.MaxCameras {
		dialog.ShowError(fmt.Errorf("Subscription Limit Reached!\n\nYour '%s' plan allows max %d cameras.\nPlease upgrade your subscription to add more.", serverSettings.Plan, serverSettings.MaxCameras), myWindow)
		return
	}

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
				dialog.ShowError(fmt.Errorf("RTSP Connection Failed:\n%v", err), myWindow)
			} else {
				addLog("RTSP Check Success: Camera is online and responding.")
				dialog.ShowInformation("Connection Success", "Camera is online and responding to RTSP requests!", myWindow)
			}
		}()
	})

	btnPreview := widget.NewButtonWithIcon("Video Preview", theme.VisibilityIcon(), func() {
		rtspURL := entryRTSP.Text
		if rtspURL == "" {
			dialog.ShowError(fmt.Errorf("Please enter an RTSP URL first."), myWindow)
			return
		}
		addLog(fmt.Sprintf("Launching local video preview for: %s", rtspURL))
		go playLocalRTSP(rtspURL)
	})
	btnPreview.Importance = widget.LowImportance

	items := []*widget.FormItem{
		widget.NewFormItem("Name", entryName),
		widget.NewFormItem("Local RTSP", entryRTSP),
		widget.NewFormItem("", container.NewHBox(layout.NewSpacer(), btnTest, btnPreview)),
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

	inst := &TunnelInstance{
		Camera:      cam,
		Status:      "Stopped",
		StatusDot:   dot,
		StatusText:  widget.NewLabel("Stopped"),
		StopChan:    make(chan bool),
		PublicLabel: widget.NewEntry(),
	}
	inst.PublicLabel.Disable() // read-only
	inst.PublicLabel.Hide()
	return inst
}

func createCameraCard(inst *TunnelInstance) fyne.CanvasObject {
	title := widget.NewLabelWithStyle(inst.Camera.Name, fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	details := widget.NewLabel(fmt.Sprintf("Target: %s", inst.Camera.LocalRTSP))
	details.Wrapping = fyne.TextTruncate

	statusBox := container.NewHBox(
		container.NewCenter(inst.StatusDot),
		inst.StatusText,
	)

	inst.PublicLabel.SetText("Public Link: N/A")
	inst.PublicLabel.TextStyle = fyne.TextStyle{Italic: true}

	btnCopy := widget.NewButtonWithIcon("", theme.ContentCopyIcon(), func() {
		myWindow.Clipboard().SetContent(inst.PublicURL)
		addLog(fmt.Sprintf("[%s] RTSP URL copied to clipboard.", inst.Camera.Name))
	})

	btnWeb := widget.NewButtonWithIcon("", theme.VisibilityIcon(), func() {
		if inst.PublicURL == "" {
			return
		}
		// Generate Web Link
		baseURL := strings.TrimSuffix(config.ServerURL, "/")
		webURL := fmt.Sprintf("%s/rtc/stream.html?src=%s&mode=mse,webrtc,hls,mp4,mjpeg", baseURL, url.QueryEscape(inst.Camera.Name))
		exec.Command("rundll32", "url.dll,FileProtocolHandler", webURL).Start()
		addLog(fmt.Sprintf("[%s] Opening Web Player in browser...", inst.Camera.Name))
	})

	publicBox := container.NewBorder(nil, nil, nil, container.NewHBox(btnCopy, btnWeb), inst.PublicLabel)

	btnToggle := widget.NewButtonWithIcon("", theme.MediaPlayIcon(), nil)
	inst.ToggleBtn = btnToggle
	btnToggle.OnTapped = func() {
		inst.mu.Lock()
		running := inst.Running
		inst.mu.Unlock()

		if !running {
			go inst.Start()
			btnToggle.SetIcon(theme.MediaStopIcon())
			btnCopy.Show()
			btnWeb.Show()
		} else {
			go inst.Stop()
			btnToggle.SetIcon(theme.MediaPlayIcon())
			btnCopy.Hide()
			btnWeb.Hide()
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
		container.NewVBox(title, details, statusBox, publicBox),
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

	btnPreview := widget.NewButtonWithIcon("Video Preview", theme.VisibilityIcon(), func() {
		addLog(fmt.Sprintf("Launching local video preview for: %s", entryRTSP.Text))
		go playLocalRTSP(entryRTSP.Text)
	})

	items := []*widget.FormItem{
		widget.NewFormItem("Name", entryName),
		widget.NewFormItem("Local RTSP", entryRTSP),
		widget.NewFormItem("", container.NewHBox(layout.NewSpacer(), btnTest, btnPreview)),
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
			client := &http.Client{Timeout: 15 * time.Second}
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

	// Create/update the L2TP VPN entry using PowerShell
	pskParam := ""
	if config.L2TPPSK != "" {
		pskParam = fmt.Sprintf("-L2tpPsk '%s'", config.L2TPPSK)
	}

	psScript := fmt.Sprintf(`
$vpnName = 'RTSP2go-VPN'
Remove-VpnConnection -Name $vpnName -Force -ErrorAction SilentlyContinue
Add-VpnConnection -Name $vpnName -ServerAddress '%s' -TunnelType L2tp %s -AuthenticationMethod MSChapv2 -EncryptionLevel Optional -Force
Set-VpnConnection -Name $vpnName -SplitTunneling $true
`, config.L2TPServer, pskParam)

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

func connectWireGuard() error {
	if config.WireGuardConfig == "" {
		return fmt.Errorf("WireGuard configuration is empty. Go to Settings and paste your wg0.conf content.")
	}

	// Save to wg0.conf inside the application directory
	currentDir, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("could not get current directory: %w", err)
	}
	confPath := filepath.Join(currentDir, "wg0.conf")
	err = ioutil.WriteFile(confPath, []byte(config.WireGuardConfig), 0600)
	if err != nil {
		return fmt.Errorf("failed to save wg0.conf: %w", err)
	}

	addLog("WireGuard: Installing and connecting tunnel...")

	// Execute wireguard install command
	// Note: wireguard command line needs the absolute path
	cmd := exec.Command("wireguard", "/installtunnelservice", confPath)
	out, err := cmd.CombinedOutput()
	if err != nil {
		// Ignore error if it's already installed, so try uninstalling and reinstalling just in case
		addLog(fmt.Sprintf("WireGuard Start Warning: %v — %s", err, string(out)))
		exec.Command("wireguard", "/uninstalltunnelservice", "wg0").Run()
		time.Sleep(1 * time.Second)
		cmd = exec.Command("wireguard", "/installtunnelservice", confPath)
		out, err = cmd.CombinedOutput()
		if err != nil {
			return fmt.Errorf("failed to install wireguard service: %v — %s", err, string(out))
		}
	}
	
	addLog("WireGuard: ✅ Tunnel active!")
	return nil
}

func disconnectWireGuard() {
	addLog("WireGuard: Stopping tunnel...")
	cmd := exec.Command("wireguard", "/uninstalltunnelservice", "wg0")
	out, err := cmd.CombinedOutput()
	if err != nil {
		addLog(fmt.Sprintf("WireGuard: Stop warning: %v — %s", err, string(out)))
	} else {
		addLog("WireGuard: Disconnected.")
	}
}

func fetchGatewaySettings() {
	apiURL := getServerAPIURL()
	if apiURL == "" {
		return
	}

	// If no credentials, we are in Test Mode
	if config.ApiUsername == "" || config.ApiPassword == "" {
		serverSettings = GatewaySettings{
			Plan:         "Trial",
			MaxCameras:   2,
			IsTrial:      true,
			LimitMinutes: 60,
		}
		addLog("Auth: ℹ️ Gateway is running in Test Broadcast Mode (Trial)")
		return
	}

	checkURL := strings.Replace(apiURL, "/api/streams", "/api/gateway/check", 1)

	client := &http.Client{Timeout: 10 * time.Second}
	req, _ := http.NewRequest("GET", checkURL, nil)
	req.SetBasicAuth(config.ApiUsername, config.ApiPassword)

	resp, err := client.Do(req)
	if err != nil {
		addLog(fmt.Sprintf("Auth: ⚠️ Connection failed: %v. Using local cache.", err))
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		var settings GatewaySettings
		if err := json.NewDecoder(resp.Body).Decode(&settings); err == nil {
			serverSettings = settings
			addLog(fmt.Sprintf("Auth: ✅ Active Plan: %s (Limit: %d cameras)", settings.Plan, settings.MaxCameras))
		}
	} else if resp.StatusCode == http.StatusUnauthorized {
		addLog("Auth: ❌ Invalid API Credentials. Please check Settings.")
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

	// Fetch latest subscription limits before starting
	go fetchGatewaySettings()

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
		} else if vpnMode == "wireguard" {
			addLog("Global: Connecting WireGuard VPN...")
			err := connectWireGuard()
			if err != nil {
				dialog.ShowError(fmt.Errorf("WireGuard Connection Failed:\n%v\nEnsure wireguard.exe is installed globally on your machine.", err), myWindow)
				addLog(fmt.Sprintf("Global ❌ WireGuard connection failed: %v", err))
				return
			}
			time.Sleep(3 * time.Second)
		} else if vpnMode == "zerotier" {
			addLog("Global: Validating ZeroTier VPN Connection...")
		}

		// Cloudflare Tunnel
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

		// If L2TP or WG mode, disconnect the VPN after stopping all streams
		vpnMode := config.VPNMode
		if vpnMode == "l2tp" {
			disconnectL2TP()
		} else if vpnMode == "wireguard" {
			disconnectWireGuard()
		}

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

	// Trial Timer Logic
	if serverSettings.IsTrial && serverSettings.LimitMinutes > 0 {
		addLog(fmt.Sprintf("[%s] Trial Mode: Broadcast limited to %d minutes.", inst.Camera.Name, serverSettings.LimitMinutes))
		go func() {
			select {
			case <-time.After(time.Duration(serverSettings.LimitMinutes) * time.Minute):
				addLog(fmt.Sprintf("[%s] ⚠️ Trial period expired (%d min). Stopping...", inst.Camera.Name, serverSettings.LimitMinutes))
				inst.Stop()
				dialog.ShowInformation("Trial Expired", fmt.Sprintf("The broadcast for '%s' has stopped because the %d-minute trial period expired.", inst.Camera.Name, serverSettings.LimitMinutes), myWindow)
			case <-inst.StopChan:
				// Stopped manually
			}
		}()
	}

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

	// Conditional deregister: only if KeepCameraOnStop is FALSE
	if !config.KeepCameraOnStop {
		go deregisterFromBackend(inst.Camera.Name)
	} else {
		addLog(fmt.Sprintf("[%s] Persistence: Camera kept on server per settings.", inst.Camera.Name))
	}

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

func playLocalRTSP(uri string) {
	// Try VLC first (Common locations)
	vlcPaths := []string{
		"C:\\Program Files\\VideoLAN\\VLC\\vlc.exe",
		"C:\\Program Files (x86)\\VideoLAN\\VLC\\vlc.exe",
		"vlc", // If in PATH
	}

	for _, p := range vlcPaths {
		cmd := exec.Command(p, uri, "--network-caching=300", "--play-and-exit")
		if err := cmd.Start(); err == nil {
			return
		}
	}

	// Try ffplay as fallback
	cmd := exec.Command("ffplay", "-i", uri, "-an", "-sn", "-fflags", "nobuffer", "-flags", "low_delay", "-window_title", "Local RTSP Preview")
	if err := cmd.Start(); err == nil {
		return
	}

	addLog("Preview Error: Could not find VLC or ffplay.")
	dialog.ShowError(fmt.Errorf("Video Player Not Found!\n\nPlease install VLC Media Player to use the local preview feature."), myWindow)
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
		// L2TP creates a PPP adapter; detect its IP using multiple fallbacks
		// We search by common aliases AND by infrastructure types (NdisPhysicalMedium 14=WAN, 8=WirelessWAN)
		psCommand = `
            $addr = Get-NetIPAddress -AddressFamily IPv4 -ErrorAction SilentlyContinue | Where-Object { 
                $_.InterfaceAlias -like '*PPP*' -or 
                $_.InterfaceAlias -like '*VPN*' -or 
                $_.InterfaceAlias -like '*L2TP*' -or 
                $_.InterfaceAlias -like '*RTSP2go*' -or
                $_.InterfaceAlias -like '*RAS*'
            } | Select-Object -First 1
            if (!$addr) {
                $iface = Get-NetIPInterface -AddressFamily IPv4 -ErrorAction SilentlyContinue | Where-Object { $_.NdisPhysicalMedium -eq 14 -or $_.NdisPhysicalMedium -eq 8 } | Select-Object -First 1
                if ($iface) { $addr = Get-NetIPAddress -InterfaceIndex $iface.InterfaceIndex -AddressFamily IPv4 | Select-Object -First 1 }
            }
            if ($addr) { $addr.IPAddress }
        `
	} else if vpnMode == "wireguard" {
		psCommand = "(Get-NetIPAddress -InterfaceAlias 'wg0','*WireGuard*' -AddressFamily IPv4 -ErrorAction SilentlyContinue | Select-Object -First 1).IPAddress"
	} else {
		// Default to ZeroTier
		psCommand = "(Get-NetIPAddress -InterfaceAlias 'ZeroTier*' -AddressFamily IPv4 -ErrorAction SilentlyContinue | Select-Object -First 1).IPAddress"
	}

	cmd := exec.Command("powershell", "-Command", psCommand)
	out, err := cmd.Output()
	if err == nil {
		ip := strings.TrimSpace(string(out))
		if ip != "" {
			return ip
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
				remote, err := net.DialTimeout("tcp", targetHostPort, 10*time.Second)
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

	// Mode Test: Jika username kosong, bypass auth di API
	isTestMode := config.ApiUsername == ""
	if isTestMode {
		if strings.Contains(apiURL, "?") {
			apiURL += "&test=true"
		} else {
			apiURL += "?test=true"
		}
		addLog(fmt.Sprintf("[%s] Registering in TEST MODE (No Credentials)...", camName))
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

		// Replace the RTSP URL so it connects to our local proxy via VPN interface
		host := vpnIP
		proxyHostPort := fmt.Sprintf("%s:%d", host, inst.proxyPort)
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

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		addLog(fmt.Sprintf("[%s] Auto-Register Failed: Cannot reach VPS backend via HTTP. (%v)", camName, err))
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusCreated {
		baseURL := strings.TrimSuffix(config.ServerURL, "/")
		webURL := fmt.Sprintf("%s/rtc/stream.html?src=%s", baseURL, url.QueryEscape(camName))

		addLog(fmt.Sprintf("[%s] Successfully registered!", camName))
		addLog(fmt.Sprintf("[%s] RTSP: %s", camName, rtspSource))
		addLog(fmt.Sprintf("[%s] WEB: %s", camName, webURL))

		inst.PublicURL = rtspSource
		inst.PublicLabel.SetText(webURL) // Show Web Link by default as it is more useful for user
		inst.PublicLabel.Show()
		inst.PublicLabel.Refresh()
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

	client := &http.Client{Timeout: 15 * time.Second}
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
			client := &http.Client{Timeout: 15 * time.Second}
			resp, err := client.Get(apiURL)
			if err != nil {
				results += "❌ RTSP2go API: UNREACHABLE\n    " + err.Error() + "\n\n"
				addLog("[Diagnose] RTSP2go API: UNREACHABLE — Auto-Register will fail!")
			} else {
				results += fmt.Sprintf("✅ RTSP2go API: OK (Code: %d)\n\n", resp.StatusCode)
				addLog(fmt.Sprintf("[Diagnose] RTSP2go API connection: Status %d", resp.StatusCode))
				resp.Body.Close()
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
