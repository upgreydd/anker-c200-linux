package main

import (
	"fmt"
	"strconv"

	"github.com/vladimirvivien/go4vl/v4l2"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"
)

func buildUI(fd uintptr, win fyne.Window, prefs fyne.Preferences, preview *canvas.Image) (fyne.CanvasObject, *ui) {
	status := widget.NewLabel("")
	status.Importance = widget.DangerImportance
	status.Wrapping = fyne.TextWrapWord
	status.Hide()
	u := &ui{fd: fd, status: status, restoreHDR: func() {}}

	settings := container.NewVBox()
	items, err := u.vendorControls(win, prefs)
	if err != nil {
		items = map[any]fyne.CanvasObject{}
		settings.Add(widget.NewLabel("Anker settings unavailable: " + err.Error()))
	}
	uvc, names, order, err := u.uvcControls(win)
	if err != nil {
		settings.Add(widget.NewLabel("Standard controls unavailable: " + err.Error()))
	}
	for k, w := range uvc {
		items[k] = w
	}

	// Grouped by what the user wants to adjust; an auto toggle always sits above the
	// manual control it locks. Missing controls are skipped, unknown ones go to "Other".
	type row struct {
		key   any
		label string
	}
	groups := []struct {
		title string
		rows  []row
	}{
		{"Framing", []row{{"fov", "Field of view"}, {v4l2.CtrlCameraZoomAbsolute, "Zoom"}, {v4l2.CtrlCameraPanAbsolute, "Pan"}, {v4l2.CtrlCameraTiltAbsolute, "Tilt"}, {"mirror", "Mirror"}}},
		{"Focus", []row{{v4l2.CtrlCameraFocusAuto, "Autofocus"}, {v4l2.CtrlCameraFocusAbsolute, "Focus"}}},
		{"Exposure", []row{{v4l2.CtrlCameraExposureAuto, "Mode"}, {v4l2.CtrlCameraExposureAbsolute, "Exposure time"}, {"hdr", "HDR"}, {v4l2.CtrlPowerlineFrequency, "Anti-flicker"}}},
		{"White balance", []row{{v4l2.CtrlAutoWhiteBalance, "Auto"}, {v4l2.CtrlWhiteBalanceTemperature, "Temperature (K)"}}},
		{"Image", []row{{v4l2.CtrlBrightness, "Brightness"}, {v4l2.CtrlContrast, "Contrast"}, {v4l2.CtrlSaturation, "Saturation"}, {v4l2.CtrlSharpness, "Sharpness"}, {v4l2.CtrlGamma, "Gamma"}, {v4l2.CtrlHue, "Hue"}}},
		{"Microphone", []row{{"mic", "Pick-up"}}},
	}
	used := map[any]bool{}
	addGroup := func(title string, rows []row) {
		form := widget.NewForm()
		for _, r := range rows {
			if w, ok := items[r.key]; ok && !used[r.key] {
				used[r.key] = true
				form.Append(r.label, w)
			}
		}
		if len(form.Items) > 0 {
			settings.Add(widget.NewCard(title, "", form))
		}
	}
	for _, g := range groups {
		addGroup(g.title, g.rows)
	}
	var other []row
	for _, id := range order {
		other = append(other, row{id, names[id]})
	}
	addGroup("Other", other)
	if reset, ok := items["reset"]; ok {
		settings.Add(reset)
	}
	// errors stay visible above the scroll area
	split := container.NewHSplit(preview, container.NewBorder(status, nil, nil, nil, container.NewVScroll(settings)))
	split.Offset = 0.6
	return split, u
}

// V4L2 control flags (videodev2.h)
const (
	ctrlFlagDisabled = 0x01
	ctrlFlagReadOnly = 0x04
	ctrlFlagInactive = 0x10
)

// ui re-reads every widget from the camera after each change, so controls the
// firmware/driver couples (auto vs manual exposure/WB/focus, ...) always show the real state.
type ui struct {
	fd         uintptr
	status     *widget.Label // shown only for errors
	refreshing bool          // set while widgets are updated programmatically, so callbacks don't write back
	refreshers []func()
	restoreHDR func() // re-applies the saved HDR choice; set by vendorControls
}

func (u *ui) done(err error) {
	if err != nil {
		u.status.SetText("⚠ " + err.Error())
		u.status.Show()
	} else {
		u.status.Hide()
	}
	u.refreshing = true
	for _, r := range u.refreshers {
		r()
	}
	u.refreshing = false
}

// onRefresh registers r and runs it once to fill the widget without writing back to the camera.
func (u *ui) onRefresh(r func()) {
	u.refreshing = true
	r()
	u.refreshing = false
	u.refreshers = append(u.refreshers, r)
}

func b2i(on bool) byte {
	if on {
		return 1
	}
	return 0
}

// radio builds a horizontal radio group for labelled int values.
func (u *ui) radio(labels map[int]string, order []int, write func(int) error) (*widget.RadioGroup, func(int)) {
	var opts []string
	for _, v := range order {
		opts = append(opts, labels[v])
	}
	r := widget.NewRadioGroup(opts, func(s string) {
		for v, l := range labels {
			if l == s && !u.refreshing {
				u.done(write(v))
			}
		}
	})
	r.Horizontal = true
	r.Required = true
	return r, func(v int) { r.SetSelected(labels[v]) }
}

func (u *ui) vendorControls(win fyne.Window, prefs fyne.Preferences) (map[any]fyne.CanvasObject, error) {
	fd := u.fd
	if _, err := xuGet(fd, selFrame); err != nil {
		return nil, err
	}
	frame := func(edit func([]byte)) error { return xuUpdate(fd, selFrame, edit) }

	fov, setFov := u.radio(map[int]string{65: "65°", 78: "78°", 95: "95°"}, []int{65, 78, 95},
		func(v int) error { return frame(func(b []byte) { packFov(b, v) }) })
	// AnkerWork values: omnidirectional = 0 (factory default), directional = 90
	mic, setMic := u.radio(map[int]string{90: "Directional 90°", 0: "Omnidirectional 360°"}, []int{90, 0},
		func(v int) error { return frame(func(b []byte) { packMic(b, v) }) })
	u.onRefresh(func() {
		if b, err := xuGet(fd, selFrame); err == nil {
			f, m := parseFrame(b)
			setFov(f)
			setMic(m)
		}
	})

	mirror := widget.NewCheck("", func(on bool) {
		if !u.refreshing {
			u.done(xuUpdate(fd, selFlip, func(b []byte) { b[0] = b2i(on) }))
		}
	})
	u.onRefresh(func() {
		if b, err := xuGet(fd, selFlip); err == nil {
			mirror.SetChecked(b[0] != 0)
		}
	})

	// The C200 always reads HDR back as 1 although writes do take effect (measured), so like
	// AnkerWork we keep the state ourselves and push it to the camera on connect.
	setHDR := func(on bool) error {
		prefs.SetBool("hdr", on)
		return xuUpdate(fd, selHDR, func(b []byte) { b[0] = b2i(on) })
	}
	hdr := widget.NewCheck("", nil)
	hdr.Checked = prefs.BoolWithFallback("hdr", true) // shortcut: assumes factory default is on (readback is always 1); measure after a reset if it matters
	// only re-apply a choice the user actually made (both fallbacks agree only when the key exists)
	hdr.Disable() // enabled by restoreHDR once the stream has settled
	u.restoreHDR = func() {
		hdr.Enable()
		if prefs.BoolWithFallback("hdr", false) == prefs.BoolWithFallback("hdr", true) {
			if err := setHDR(hdr.Checked); err != nil {
				u.done(err)
			}
		}
	}
	hdr.OnChanged = func(on bool) { u.done(setHDR(on)) }

	reset := widget.NewButton("Reset camera to factory defaults…", func() {
		dialog.ShowConfirm("Factory reset",
			"All camera settings go back to factory defaults\n(95° field of view, omnidirectional mic, auto exposure, …).\n\nThe camera restarts; the preview returns in a few seconds.",
			func(ok bool) {
				if ok {
					prefs.RemoveValue("hdr") // back to the camera's default too
					u.done(xuUpdate(fd, selRestore, func(b []byte) { clear(b); b[0], b[1] = 1, 1 }))
				}
			}, win)
	})
	reset.Importance = widget.DangerImportance

	return map[any]fyne.CanvasObject{"fov": fov, "mic": mic, "hdr": hdr, "mirror": mirror, "reset": reset}, nil
}

// menuName names menu values the camera does not list itself (the C200 lists only
// "Manual Mode" for auto_exposure although it powers up in Auto).
func menuName(id v4l2.CtrlID, v int32) string {
	if id == v4l2.CtrlCameraExposureAuto && v == 0 {
		return "Auto Mode"
	}
	return strconv.Itoa(int(v))
}

// valueText formats slider values in the units UVC defines.
func valueText(id v4l2.CtrlID, v int32) string {
	switch id {
	case v4l2.CtrlCameraPanAbsolute, v4l2.CtrlCameraTiltAbsolute: // arc-seconds
		return strconv.Itoa(int(v/3600)) + "°"
	case v4l2.CtrlCameraExposureAbsolute: // 100 µs
		return strconv.FormatFloat(float64(v)/10, 'f', 1, 64) + " ms"
	case v4l2.CtrlCameraZoomAbsolute: // the C200 reports 100..400 for 1×..4×
		return strconv.FormatFloat(float64(v)/100, 'f', 2, 64) + "×"
	}
	return strconv.Itoa(int(v))
}

// uvcControls builds a widget for every standard control the camera reports,
// keyed by control ID, plus the driver's names in driver order.
func (u *ui) uvcControls(win fyne.Window) (items map[any]fyne.CanvasObject, names map[v4l2.CtrlID]string, order []v4l2.CtrlID, err error) {
	fd := u.fd
	items, names = map[any]fyne.CanvasObject{}, map[v4l2.CtrlID]string{}
	ctrls, err := v4l2.QueryAllControls(fd)
	if err != nil {
		return nil, nil, nil, err
	}
	add := func(c v4l2.Control, w fyne.CanvasObject) {
		items[c.ID], names[c.ID] = w, c.Name
		order = append(order, c.ID)
	}
	for _, c := range ctrls {
		id := c.ID
		if _, err := v4l2.GetControlValue(fd, id); err != nil || c.Flags&ctrlFlagDisabled != 0 {
			continue // write-only, class headers, disabled
		}
		set := func(v int32) {
			if !u.refreshing {
				u.done(v4l2.SetControlValue(fd, id, v4l2.CtrlValue(v)))
			}
		}
		var w fyne.Disableable
		var update func(cur int32)
		var val *widget.Label // slider value text, shows "auto" while the control is inactive
		switch {
		case c.Type == v4l2.CtrlTypeBool:
			chk := widget.NewCheck("", func(on bool) { set(int32(b2i(on))) })
			update = func(cur int32) { chk.SetChecked(cur != 0) }
			w = chk
			add(c, chk)
		case c.IsMenu():
			items, _ := c.GetMenuItems()
			sel := widget.NewSelect(nil, nil)
			var byName map[string]int32
			var cur int32
			listed := func(v int32) bool {
				for _, it := range items {
					if int32(it.Index) == v {
						return true
					}
				}
				return false
			}
			update = func(v int32) {
				cur, byName, sel.Options = v, map[string]int32{}, nil
				for _, it := range items {
					byName[it.Name] = int32(it.Index)
					sel.Options = append(sel.Options, it.Name)
				}
				if !listed(v) { // e.g. C200 auto_exposure=Auto: valid at power-up but not offered by the camera
					byName[menuName(id, v)] = v
					sel.Options = append(sel.Options, menuName(id, v))
				}
				for n, nv := range byName {
					if nv == v {
						sel.SetSelected(n)
					}
				}
			}
			sel.OnChanged = func(s string) {
				if u.refreshing {
					return
				}
				v := byName[s]
				if listed(cur) {
					set(v)
					return
				}
				msg := fmt.Sprintf("The camera does not offer %q as a choice, so it only comes back after\nreplugging the camera or a factory reset. Continue?", menuName(id, cur))
				dialog.ShowConfirm(c.Name, msg, func(ok bool) {
					if ok {
						set(v)
					} else {
						u.done(nil) // revert the selection
					}
				}, win)
			}
			w = sel
			add(c, sel)
		case c.Type == v4l2.CtrlTypeInt:
			s := widget.NewSlider(float64(c.Minimum), float64(c.Maximum))
			s.Step = float64(max(c.Step, 1))
			val = widget.NewLabel("")
			s.OnChanged = func(f float64) { val.SetText(valueText(id, int32(f))) }
			s.OnChangeEnded = func(f float64) { set(int32(f)) }
			update = func(cur int32) { s.SetValue(float64(cur)); val.SetText(valueText(id, cur)) } // SetValue skips OnChanged when unchanged
			w = s
			// fixed-width value label so the slider doesn't jump while dragging
			add(c, container.NewBorder(nil, nil, nil, container.NewGridWrap(fyne.NewSize(64, val.MinSize().Height), val), s))
		default:
			continue
		}
		u.onRefresh(func() {
			cur, err := v4l2.GetControlValue(fd, id)
			info, ierr := v4l2.QueryControlInfo(fd, id)
			if err != nil || ierr != nil {
				return
			}
			update(int32(cur))
			if info.Flags&(ctrlFlagInactive|ctrlFlagReadOnly) != 0 {
				w.Disable()
				if val != nil && info.Flags&ctrlFlagInactive != 0 {
					val.SetText("auto") // the camera reports meaningless values here (e.g. 168 K)
				}
			} else {
				w.Enable()
			}
		})
	}
	return items, names, order, nil
}
