package main

import (
	"context"
	"fmt"
	"image"
	"os"
	"path/filepath"
	"strings"
	"unsafe"

	"github.com/vladimirvivien/go4vl/device"
	"github.com/vladimirvivien/go4vl/v4l2"

	"golang.org/x/sys/unix"
)

// UVC extension unit 6 protocol, reverse-engineered from AnkerWork (AWDeviceInterfaceCamera::*).
const (
	xuUnit     = 6
	selFrame   = 0x10 // [mode, 1, fov16, 0, micPickup16, ...]
	selFlip    = 0x11
	selHDR     = 0x13
	selRestore = 0x0d // [1,1,0...] = factory reset (AWDeviceInterfaceCamera::restoreDevice)
	getCur     = 0x81
	setCur     = 0x01
	getLen     = 0x85
	previewW   = 640
	previewH   = 360
)

type xuQuery struct {
	unit, selector, query uint8
	size                  uint16
	data                  unsafe.Pointer
}

var uvciocCtrlQuery = uintptr(3<<30 | unsafe.Sizeof(xuQuery{})<<16 | 'u'<<8 | 0x21)

func xu(fd uintptr, sel, query uint8, buf []byte) error {
	q := xuQuery{xuUnit, sel, query, uint16(len(buf)), unsafe.Pointer(&buf[0])}
	if _, _, e := unix.Syscall(unix.SYS_IOCTL, fd, uvciocCtrlQuery, uintptr(unsafe.Pointer(&q))); e != 0 {
		return fmt.Errorf("XU sel 0x%02x query 0x%02x: %w", sel, query, e)
	}
	return nil
}

func xuGet(fd uintptr, sel uint8) ([]byte, error) {
	n := make([]byte, 2)
	if err := xu(fd, sel, getLen, n); err != nil {
		return nil, err
	}
	buf := make([]byte, int(n[0])|int(n[1])<<8)
	return buf, xu(fd, sel, getCur, buf)
}

// xuUpdate does read-modify-write, like the vendor app.
func xuUpdate(fd uintptr, sel uint8, edit func([]byte)) error {
	b, err := xuGet(fd, sel)
	if err != nil {
		return err
	}
	edit(b)
	return xu(fd, sel, setCur, b)
}

// Byte layout of selector 0x10, copied from AWDeviceInterfaceCamera::setFov / setDirectionalPickUp.
func packFov(b []byte, fov int)          { b[0], b[1], b[2], b[3], b[4] = 0, 1, byte(fov), byte(fov>>8), 0 }
func packMic(b []byte, deg int)          { b[3], b[5], b[6] = 0, byte(deg), byte(deg>>8) }
func parseFrame(b []byte) (fov, mic int) { return int(b[2]) | int(b[3])<<8, int(b[5]) | int(b[6])<<8 }

func findCamera() (string, error) {
	names, _ := filepath.Glob("/sys/class/video4linux/video*/name")
	for _, p := range names {
		if n, _ := os.ReadFile(p); strings.Contains(string(n), "C200") {
			dev := "/dev/" + filepath.Base(filepath.Dir(p))
			// the metadata node of the same camera rejects XU queries; we want the capture node
			if f, err := os.OpenFile(dev, os.O_RDWR, 0); err == nil {
				_, err = xuGet(f.Fd(), selHDR)
				f.Close()
				if err == nil {
					return dev, nil
				}
			}
		}
	}
	return "", fmt.Errorf("Anker PowerConf C200 not found (is it plugged in, are you in group 'video'?)")
}

func yuyvToImage(frame []byte, w, h int) *image.YCbCr {
	img := image.NewYCbCr(image.Rect(0, 0, w, h), image.YCbCrSubsampleRatio422)
	for i := 0; i < w*h/2; i++ {
		p := frame[i*4 : i*4+4]
		img.Y[i*2], img.Cb[i], img.Y[i*2+1], img.Cr[i] = p[0], p[1], p[2], p[3]
	}
	return img
}

func openCamera(path string) (*device.Device, error) {
	cam, err := device.Open(path, device.WithPixFormat(v4l2.PixFormat{
		PixelFormat: v4l2.PixelFmtYUYV, Width: previewW, Height: previewH, Field: v4l2.FieldNone}))
	if err != nil {
		return nil, err
	}
	// streaming must run: the camera ignores some writes (e.g. mirror) while idle
	if err := cam.Start(context.Background()); err != nil {
		cam.Close()
		return nil, err
	}
	return cam, nil
}
