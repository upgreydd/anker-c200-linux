// anker-c200: live preview + settings for Anker PowerConf C200 (291a:3369) on Linux.
//
// Vendor controls go through UVC extension unit 6 (reverse-engineered from AnkerWork
// AWDeviceInterface.dll); everything else is the camera's standard UVC controls.
package main

import (
	_ "embed"
	"errors"
	"fmt"
	"image"
	"os"
	"time"

	"github.com/vladimirvivien/go4vl/device"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
	"golang.org/x/sys/unix"
)

// version is set at release build time via -ldflags "-X main.version=..."
var version = "dev"

//go:embed packaging/anker-c200.svg
var iconSVG []byte

func main() {
	fixedPath := "" // with an explicit node we never search; after a replug the node may change
	if len(os.Args) > 1 {
		if os.Args[1] == "--version" {
			fmt.Println("anker-c200", version)
			return
		}
		fixedPath = os.Args[1]
	}
	// the ID matches the .desktop file name (Wayland app_id); the constant title is the
	// X11 WM_CLASS GLFW derives, matched by StartupWMClass
	a := app.NewWithID("io.github.upgreydd.anker-c200")
	a.SetIcon(fyne.NewStaticResource("anker-c200.svg", iconSVG))
	win := a.NewWindow("Anker PowerConf C200")
	preview := canvas.NewImageFromImage(image.NewYCbCr(image.Rect(0, 0, previewW, previewH), image.YCbCrSubsampleRatio422))
	preview.FillMode = canvas.ImageFillContain
	preview.SetMinSize(fyne.NewSize(previewW, previewH))
	msg := widget.NewLabel("Looking for the camera…")
	waiting := container.NewCenter(msg)
	win.SetContent(waiting)

	// Connection loop: the camera restarts after a factory reset and may be replugged,
	// so reconnect and rebuild the UI whenever the stream ends.
	go func() {
		show := func(s string) {
			fyne.DoAndWait(func() {
				for _, o := range win.Canvas().Overlays().List() { // open dialogs would act on a stale fd
					win.Canvas().Overlays().Remove(o)
				}
				msg.SetText(s)
				win.SetContent(waiting)
			})
		}
		for {
			path, err := fixedPath, error(nil)
			if path == "" {
				path, err = findCamera()
			}
			var cam *device.Device
			if err == nil {
				cam, err = openCamera(path)
			}
			if err != nil {
				hint := ""
				if errors.Is(err, unix.EBUSY) {
					hint = "\nAnother app is using the camera. Close it — the preview needs the stream."
				}
				show(err.Error() + hint + "\n\nRetrying…")
				time.Sleep(2 * time.Second)
				continue
			}
			var u *ui
			frames := 0
			for frame := range cam.GetOutput() { // closes when the camera disappears
				if len(frame) != previewW*previewH*2 { // also guards against the driver picking another format
					continue
				}
				switch frames++; frames {
				case 1: // build once the stream runs
					fyne.DoAndWait(func() {
						var content fyne.CanvasObject
						content, u = buildUI(cam.Fd(), win, a.Preferences(), preview)
						win.SetContent(content)
					})
				case 60: // the C200 firmware hung (USB timeouts) on an HDR write right after stream start; wait ~2-4 s
					fyne.DoAndWait(func() { u.restoreHDR() })
				}
				img := yuyvToImage(frame, previewW, previewH)
				fyne.Do(func() { preview.Image = img; preview.Refresh() })
			}
			show("Camera disconnected or restarting — waiting for it to come back…") // detaches widgets before fd closes
			cam.Close()
			time.Sleep(time.Second)
		}
	}()

	win.Resize(fyne.NewSize(1200, 720))
	win.ShowAndRun()
}
