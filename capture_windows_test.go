//go:build windows

package main

import (
	"bytes"
	"fmt"
	"os"
	"runtime"
	"testing"
	"time"
	"unsafe"
)

func TestCaptureAspectFit(t *testing.T) {
	x, y, w, h := capAspectFit(1920, 1080, 320, 240)
	if got := [4]int{x, y, w, h}; got != [4]int{0, 30, 320, 180} {
		t.Fatalf("aspect fit = %v, want [0 30 320 180]", got)
	}
	x, y, w, h = capAspectFit(1080, 1920, 320, 240)
	if got := [4]int{x, y, w, h}; got != [4]int{92, 0, 135, 240} {
		t.Fatalf("portrait aspect fit = %v, want [92 0 135 240]", got)
	}
	x, y, w, h = capAspectFit(0, 1, 320, 240)
	if got := [4]int{x, y, w, h}; got != [4]int{} {
		t.Fatalf("invalid aspect fit = %v", got)
	}
}

func TestCaptureOptionsAndLatestResult(t *testing.T) {
	if got := capNormalizeOptions(captureOptions{}); got != (captureOptions{320, 180, 5}) {
		t.Fatalf("defaults = %+v", got)
	}
	if got := capNormalizeOptions(captureOptions{10000, 10000, 1000}); got != (captureOptions{640, 480, 30}) {
		t.Fatalf("limits = %+v", got)
	}
	out := make(chan captureResult, 1)
	capSend(out, captureResult{width: 1})
	capSend(out, captureResult{width: 2})
	if got := <-out; got.width != 2 {
		t.Fatal("did not replace stale image")
	}
	capSend(out, captureResult{width: 3})
	want := fmt.Errorf("capture stopped")
	capSend(out, captureResult{err: want})
	if got := <-out; got.err != want {
		t.Fatal("terminal error lost behind image")
	}
}

func TestCaptureSamplePaddedRows(t *testing.T) {
	// Two rows, two BGRA pixels per row, with four bytes of padding.
	src := []byte{1, 2, 3, 0, 4, 5, 6, 0, 99, 99, 99, 99, 7, 8, 9, 0, 10, 11, 12, 0, 99, 99, 99, 99}
	got := capSampleBGRA(unsafe.Pointer(&src[0]), 12, 2, 2, 4, 2)
	want := []byte{0, 0, 0, 255, 1, 2, 3, 255, 4, 5, 6, 255, 0, 0, 0, 255, 0, 0, 0, 255, 7, 8, 9, 255, 10, 11, 12, 255, 0, 0, 0, 255}
	if !bytes.Equal(got, want) {
		t.Fatalf("sample = %v, want %v", got, want)
	}
	runtime.KeepAlive(src)
}

// Opt in because this creates and resizes a visible desktop window and needs
// Windows 11 24H2+ with a working graphics device and capture permissions.
func TestCaptureIntegration(t *testing.T) {
	if os.Getenv("SPENCER_CLICKER_TEST_CAPTURE") != "1" {
		t.Skip("set SPENCER_CLICKER_TEST_CAPTURE=1 for real WGC capture")
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	hwnd := makeFixture(t, true)
	defer func() {
		if isWindow(hwnd) {
			procDestroyWindow.Call(hwnd)
		}
	}()
	pumpFor(100 * time.Millisecond)
	target := targetWindow{hwnd: hwnd, pid: windowPID(hwnd)}
	start := func(fps int) *windowCapture {
		c := startWindowCapture(target, captureOptions{320, 180, fps})
		t.Cleanup(c.stop)
		return c
	}
	animate := false
	waitFrame := func(c *windowCapture, match func(captureResult) bool) captureResult {
		t.Helper()
		deadline := time.Now().Add(8 * time.Second)
		count := 0
		var last captureResult
		nextPaint := time.Now()
		for time.Now().Before(deadline) {
			if animate && time.Now().After(nextPaint) {
				setWindowText(fixture.label, fmt.Sprintf("Live resize frame %d", time.Now().UnixNano()))
				nextPaint = time.Now().Add(100 * time.Millisecond)
			}
			pumpFor(10 * time.Millisecond)
			select {
			case frame, ok := <-c.frames:
				if !ok {
					t.Fatal("capture ended without expected frame")
				}
				if frame.err != nil {
					t.Fatal(frame.err)
				}
				if frame.width != 320 || frame.height != 180 || len(frame.pixels) != 320*180*4 {
					t.Fatalf("bad frame: %dx%d, %d bytes", frame.width, frame.height, len(frame.pixels))
				}
				count++
				last = frame
				if match(frame) {
					return frame
				}
			default:
			}
		}
		if len(last.pixels) > 0 {
			left, right := 320, -1
			for x := 0; x < 320; x++ {
				i := (90*320 + x) * 4
				if last.pixels[i] > 20 || last.pixels[i+1] > 20 || last.pixels[i+2] > 20 {
					left = min(left, x)
					right = max(right, x)
				}
			}
			t.Logf("received %d unmatched frames; middle-row nonblack range %d..%d", count, left, right)
		}
		t.Fatalf("timed out waiting for matching WGC frame (%d received)", count)
		return captureResult{}
	}
	stop := func(c *windowCapture) {
		t.Helper()
		before := time.Now()
		c.stop()
		c.stop()
		if time.Since(before) > 100*time.Millisecond {
			t.Fatal("stop blocked the caller")
		}
		deadline := time.Now().Add(3 * time.Second)
		for time.Now().Before(deadline) {
			select {
			case <-c.done:
				return
			default:
				pumpFor(10 * time.Millisecond)
			}
		}
		t.Fatal("capture resources did not finish closing")
	}
	nonempty := func(f captureResult) bool {
		n := 0
		for i := 0; i < len(f.pixels); i += 4 {
			if f.pixels[i] > 20 || f.pixels[i+1] > 20 || f.pixels[i+2] > 20 {
				n++
			}
		}
		return n > f.width*f.height/4
	}
	c := start(5)
	first := waitFrame(c, nonempty)
	setWindowText(fixture.label, "CHANGED PIXELS\r\nCapture is receiving live content\r\nABCDEFGHIJKLMNOPQRSTUVWXYZ\r\n012345678901234567890123456789\r\nCHANGED PIXELS")
	_ = waitFrame(c, func(f captureResult) bool { return nonempty(f) && !bytes.Equal(f.pixels, first.pixels) })
	t.Log("received nonempty BGRA frame and changed source pixels")
	// Portrait resize changes letterbox geometry, proving Recreate delivered
	// a correctly sized new source rather than stale or stretched old data.
	// Keep the source changing: WGC need not emit unchanged desktop content.
	animate = true
	if ok, _, err := procSetWindowPos.Call(hwnd, 0, 0, 0, 300, 500, 0x0002|0x0004); ok == 0 {
		t.Fatal(err)
	}
	portrait := waitFrame(c, func(f captureResult) bool {
		i := (f.height/2*f.width + 50) * 4
		return nonempty(f) && f.pixels[i] == 0 && f.pixels[i+1] == 0 && f.pixels[i+2] == 0
	})
	if bytes.Equal(first.pixels, portrait.pixels) {
		t.Fatal("resize returned unchanged pixels")
	}
	if ok, _, err := procSetWindowPos.Call(hwnd, 0, 0, 0, 700, 300, 0x0002|0x0004); ok == 0 {
		t.Fatal(err)
	}
	_ = waitFrame(c, func(f captureResult) bool {
		i := (f.height/2*f.width + 50) * 4
		return nonempty(f) && (f.pixels[i] > 20 || f.pixels[i+1] > 20 || f.pixels[i+2] > 20)
	})
	t.Log("capture survived portrait and landscape resize")
	animate = false
	stop(c)
	for _, fps := range []int{1, 30} {
		c = start(fps)
		_ = waitFrame(c, nonempty)
		stop(c)
	}
	c = start(5)
	_ = waitFrame(c, nonempty)
	// Leave a queued frame: terminal error must replace it after target exit.
	pumpFor(250 * time.Millisecond)
	procDestroyWindow.Call(hwnd)
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		pumpFor(10 * time.Millisecond)
		select {
		case <-c.done:
			result, ok := <-c.frames
			if !ok || result.err == nil {
				t.Fatal("target exit did not deliver terminal error")
			}
			stop(c)
			t.Log("repeated stop/restart and target-close cleanup passed")
			return
		default:
		}
	}
	t.Fatal("target exit did not terminate capture")
}

func TestCaptureFrameDeadline(t *testing.T) {
	if got := capFrameDeadline(20); got != 50*time.Millisecond {
		t.Fatalf("deadline = %v, want 50ms", got)
	}
	if got := capFrameDeadline(0); got != time.Second {
		t.Fatalf("zero fps deadline = %v, want 1s", got)
	}
}

func TestCaptureGUIDs(t *testing.T) {
	if capIIDGraphicsCaptureItemInterop.Data1 != 0x3628e81b || capIIDGraphicsCaptureItem.Data1 != 0x79c3f95b {
		t.Fatal("unexpected Windows Graphics Capture ABI GUID")
	}
	if d := unsafe.Sizeof(capGUID{}); d != 16 {
		t.Fatalf("GUID size = %d, want 16", d)
	}
}
