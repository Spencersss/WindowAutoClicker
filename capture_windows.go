//go:build windows

package main

// WGC and D3D11 use their native ABI without cgo. All resources belong to
// one locked MTA worker thread; the UI receives only small BGRA images.
import (
	"fmt"
	"runtime"
	"sync"
	"syscall"
	"time"
	"unsafe"
)

type captureOptions struct{ width, height, fps int }
type captureResult struct {
	pixels        []byte
	width, height int
	err           error
}
type windowCapture struct {
	frames   <-chan captureResult
	done     <-chan struct{}
	stopOnce sync.Once
	cancel   chan struct{}
}
type capGUID struct {
	Data1        uint32
	Data2, Data3 uint16
	Data4        [8]byte
}
type capSize struct{ Width, Height int32 }
type capTextureDesc struct {
	Width, Height, MipLevels, ArraySize, Format uint32
	SampleCount, SampleQuality                  uint32
	Usage, BindFlags, CPUAccessFlags, MiscFlags uint32
}
type capMapped struct {
	Data                 unsafe.Pointer
	RowPitch, DepthPitch uint32
}

const (
	dxgiFormatBGRA8  = 87
	maxCaptureWidth  = 8192
	maxCaptureHeight = 8192
	maxCaptureArea   = 33554432
)

var (
	capCombase                = syscall.NewLazyDLL("combase.dll")
	capRoInitialize           = capCombase.NewProc("RoInitialize")
	capRoUninitialize         = capCombase.NewProc("RoUninitialize")
	capRoGetActivationFactory = capCombase.NewProc("RoGetActivationFactory")
	capWindowsCreateString    = capCombase.NewProc("WindowsCreateString")
	capWindowsDeleteString    = capCombase.NewProc("WindowsDeleteString")
	capD3D11                  = syscall.NewLazyDLL("d3d11.dll")
	// WinRT apartment teardown can unload GraphicsCapture.dll while its
	// process-wide window callbacks are still registered. Retain its code
	// module like the other LazyDLL imports, independently of COM objects.
	capGraphicsCapture                    = syscall.NewLazyDLL("GraphicsCapture.dll")
	capD3D11CreateDevice                  = capD3D11.NewProc("D3D11CreateDevice")
	capWrapDevice                         = capD3D11.NewProc("CreateDirect3D11DeviceFromDXGIDevice")
	capIIDGraphicsCaptureItemInterop      = capGUID{0x3628e81b, 0x3cac, 0x4c60, [8]byte{0xb7, 0xf4, 0x23, 0xce, 0x0e, 0x0c, 0x33, 0x56}}
	capIIDGraphicsCaptureItem             = capGUID{0x79c3f95b, 0x31f7, 0x4ec2, [8]byte{0xa4, 0x64, 0x63, 0x2e, 0xf5, 0xd3, 0x07, 0x60}}
	capIIDGraphicsCaptureSession5         = capGUID{0x67c0ea62, 0x1f85, 0x5061, [8]byte{0x92, 0x5a, 0x23, 0x9b, 0xe0, 0xac, 0x09, 0xcb}}
	capIIDGraphicsCaptureFramePoolStatics = capGUID{0x589b103f, 0x6bbc, 0x5df5, [8]byte{0xa9, 0x91, 0x02, 0xe2, 0x8b, 0x3b, 0x66, 0xd5}}
	capIIDDXGIDevice                      = capGUID{0x54ec77fa, 0x1377, 0x44e6, [8]byte{0x8c, 0x32, 0x88, 0xfd, 0x5f, 0x44, 0xc8, 0x4c}}
	capIIDSurfaceAccess                   = capGUID{0xa9b3d012, 0x3df2, 0x4ee3, [8]byte{0xb8, 0xd1, 0x86, 0x95, 0xf4, 0x57, 0xd3, 0xc1}}
	capIIDTexture2D                       = capGUID{0x6f15aaf2, 0xd208, 0x4e89, [8]byte{0x9a, 0xb4, 0x48, 0x95, 0x35, 0xd3, 0x4f, 0x9c}}
	capIIDClosable                        = capGUID{0x30d5a829, 0x7fa4, 0x4026, [8]byte{0x83, 0xbb, 0xd7, 0x5b, 0xae, 0x4e, 0xa9, 0x9e}}
)

// Keep converted pointers alive and on the heap across this wrapper's
// variadic allocation and possible Go stack growth.
//
//go:uintptrescapes
func capCall(p unsafe.Pointer, index int, args ...uintptr) uintptr {
	v := *(*unsafe.Pointer)(p)
	fn := *(*uintptr)(unsafe.Add(v, uintptr(index)*unsafe.Sizeof(uintptr(0))))
	a := append([]uintptr{uintptr(p)}, args...)
	r, _, _ := syscall.SyscallN(fn, a...)
	runtime.KeepAlive(p)
	return r
}
func capError(operation string, hr uintptr) error {
	if int32(hr) < 0 {
		return fmt.Errorf("%s: 0x%08x", operation, uint32(hr))
	}
	return nil
}
func capQI(p unsafe.Pointer, iid *capGUID, out *unsafe.Pointer) error {
	return capError("QueryInterface", capCall(p, 0, uintptr(unsafe.Pointer(iid)), uintptr(unsafe.Pointer(out))))
}
func capRelease(p unsafe.Pointer) {
	if p != nil {
		capCall(p, 2)
	}
}
func capCloseRelease(p unsafe.Pointer) {
	if p == nil {
		return
	}
	var closer unsafe.Pointer
	if capQI(p, &capIIDClosable, &closer) == nil {
		capCall(closer, 6)
		capRelease(closer)
	}
	capRelease(p)
}
func capRuntimeFactory(name string, iid *capGUID) (unsafe.Pointer, error) {
	u, err := syscall.UTF16FromString(name)
	if err != nil {
		return nil, err
	}
	var h uintptr
	hr, _, _ := capWindowsCreateString.Call(uintptr(unsafe.Pointer(&u[0])), uintptr(len(u)-1), uintptr(unsafe.Pointer(&h)))
	if err := capError("WindowsCreateString", hr); err != nil {
		return nil, err
	}
	defer capWindowsDeleteString.Call(h)
	var p unsafe.Pointer
	hr, _, _ = capRoGetActivationFactory.Call(h, uintptr(unsafe.Pointer(iid)), uintptr(unsafe.Pointer(&p)))
	return p, capError("activate "+name, hr)
}
func capNormalizeOptions(opt captureOptions) captureOptions {
	if opt.width < 1 {
		opt.width = 320
	}
	if opt.height < 1 {
		opt.height = 180
	}
	if opt.fps < 1 {
		opt.fps = 5
	}
	opt.width = min(opt.width, 640)
	opt.height = min(opt.height, 480)
	opt.fps = min(opt.fps, 30)
	return opt
}
func startWindowCapture(target targetWindow, options captureOptions) *windowCapture {
	out := make(chan captureResult, 1)
	done := make(chan struct{})
	c := &windowCapture{frames: out, done: done, cancel: make(chan struct{})}
	go func() {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		defer close(done)
		defer close(out)
		if err := capWorker(c.cancel, target, capNormalizeOptions(options), out); err != nil {
			capSend(out, captureResult{err: err})
		}
	}()
	return c
}
func (c *windowCapture) stop() {
	if c != nil {
		c.stopOnce.Do(func() { close(c.cancel) })
	}
}

// Single producer, latest frame wins. Errors replace a queued image too.
func capSend(out chan captureResult, r captureResult) {
	select {
	case out <- r:
		return
	default:
	}
	select {
	case <-out:
	default:
	}
	out <- r
}
func capValidSize(s capSize) bool {
	return s.Width > 0 && s.Height > 0 && s.Width <= maxCaptureWidth && s.Height <= maxCaptureHeight && int64(s.Width)*int64(s.Height) <= maxCaptureArea
}

// Windows x64 passes the eight-byte SizeInt32 struct by value in one slot.
func capSizeValue(s capSize) uintptr {
	return uintptr(uint64(uint32(s.Width)) | uint64(uint32(s.Height))<<32)
}

func capWorker(cancel <-chan struct{}, target targetWindow, opt captureOptions, out chan captureResult) error {
	if unsafe.Sizeof(uintptr(0)) != 8 {
		return fmt.Errorf("window capture requires 64-bit Windows")
	}
	select {
	case <-cancel:
		return nil
	default:
	}
	hr, _, _ := capRoInitialize.Call(1)
	if err := capError("RoInitialize MTA", hr); err != nil {
		return err
	}
	defer capRoUninitialize.Call()
	if err := capGraphicsCapture.Load(); err != nil {
		return fmt.Errorf("load Windows Graphics Capture: %w", err)
	}
	validTarget := func() bool { return target.hwnd != 0 && isWindow(target.hwnd) && windowPID(target.hwnd) == target.pid }
	if !validTarget() {
		return fmt.Errorf("capture target is no longer valid")
	}
	item, err := capCreateItem(target.hwnd)
	if err != nil {
		return err
	}
	defer capRelease(item)
	var size capSize
	if err := capError("capture item size", capCall(item, 7, uintptr(unsafe.Pointer(&size)))); err != nil {
		return err
	}
	if !capValidSize(size) {
		return fmt.Errorf("capture source dimensions are invalid or exceed safety limit")
	}
	var dev, ctx unsafe.Pointer
	hr, _, _ = capD3D11CreateDevice.Call(0, 1, 0, 0x20, 0, 0, 7, uintptr(unsafe.Pointer(&dev)), 0, uintptr(unsafe.Pointer(&ctx)))
	if err := capError("D3D11CreateDevice", hr); err != nil {
		return err
	}
	defer capRelease(dev)
	defer capRelease(ctx)
	var dxgi unsafe.Pointer
	if err := capQI(dev, &capIIDDXGIDevice, &dxgi); err != nil {
		return err
	}
	var winrtDev unsafe.Pointer
	hr, _, _ = capWrapDevice.Call(uintptr(dxgi), uintptr(unsafe.Pointer(&winrtDev)))
	capRelease(dxgi)
	if err := capError("CreateDirect3D11DeviceFromDXGIDevice", hr); err != nil {
		return err
	}
	defer capCloseRelease(winrtDev)
	factory, err := capRuntimeFactory("Windows.Graphics.Capture.Direct3D11CaptureFramePool", &capIIDGraphicsCaptureFramePoolStatics)
	if err != nil {
		return err
	}
	var pool unsafe.Pointer
	hr = capCall(factory, 6, uintptr(winrtDev), dxgiFormatBGRA8, 2, capSizeValue(size), uintptr(unsafe.Pointer(&pool)))
	capRelease(factory)
	if err := capError("CreateFreeThreaded", hr); err != nil {
		return err
	}
	defer capCloseRelease(pool)
	var session unsafe.Pointer
	if err := capError("CreateCaptureSession", capCall(pool, 10, uintptr(item), uintptr(unsafe.Pointer(&session)))); err != nil {
		return err
	}
	defer capCloseRelease(session)
	var s5 unsafe.Pointer
	if err := capQI(session, &capIIDGraphicsCaptureSession5, &s5); err != nil {
		return fmt.Errorf("preview FPS control requires Windows 11 24H2 or newer (GraphicsCaptureSession5): %w", err)
	}
	interval := (10_000_000 + uint64(opt.fps) - 1) / uint64(opt.fps)
	err = capError("set capture MinUpdateInterval", capCall(s5, 7, uintptr(interval)))
	var actual int64
	if err == nil {
		err = capError("read capture MinUpdateInterval", capCall(s5, 6, uintptr(unsafe.Pointer(&actual))))
	}
	capRelease(s5)
	if err != nil {
		return err
	}
	if actual != int64(interval) {
		return fmt.Errorf("capture did not accept requested FPS interval")
	}
	if err := capError("StartCapture", capCall(session, 6)); err != nil {
		return err
	}
	var staging unsafe.Pointer
	defer func() { capRelease(staging) }()
	var stagingSize capSize
	ticker := time.NewTicker(capFrameDeadline(opt.fps))
	defer ticker.Stop()
	for {
		select {
		case <-cancel:
			return nil
		case <-ticker.C:
			if !validTarget() {
				return fmt.Errorf("capture target is no longer valid")
			}
			// Drain at most the two pool buffers per tick, keeping the latest.
			var frame unsafe.Pointer
			for i := 0; i < 2; i++ {
				var next unsafe.Pointer
				err := capError("TryGetNextFrame", capCall(pool, 7, uintptr(unsafe.Pointer(&next))))
				if err != nil {
					capCloseRelease(frame)
					return err
				}
				if next == nil {
					break
				}
				capCloseRelease(frame)
				frame = next
			}
			if frame == nil {
				continue
			}
			var contentSize capSize
			err := capError("frame ContentSize", capCall(frame, 8, uintptr(unsafe.Pointer(&contentSize))))
			if err != nil {
				capCloseRelease(frame)
				return err
			}
			if contentSize.Width < 1 || contentSize.Height < 1 {
				capCloseRelease(frame)
				continue
			}
			if !capValidSize(contentSize) {
				capCloseRelease(frame)
				return fmt.Errorf("capture source dimensions exceed safety limit")
			}
			if contentSize != size {
				// Recreate after closing frames: old texture and new ContentSize
				// can disagree on the resize transition frame.
				capCloseRelease(frame)
				if err := capError("frame pool Recreate", capCall(pool, 6, uintptr(winrtDev), dxgiFormatBGRA8, 2, capSizeValue(contentSize))); err != nil {
					return err
				}
				size = contentSize
				continue
			}
			pixels, err := capReadFrame(dev, ctx, frame, contentSize, opt, &staging, &stagingSize)
			capCloseRelease(frame)
			if err != nil {
				return err
			}
			capSend(out, captureResult{pixels: pixels, width: opt.width, height: opt.height})
		}
	}
}
func capCreateItem(hwnd uintptr) (unsafe.Pointer, error) {
	f, err := capRuntimeFactory("Windows.Graphics.Capture.GraphicsCaptureItem", &capIIDGraphicsCaptureItemInterop)
	if err != nil {
		return nil, err
	}
	defer capRelease(f)
	var p unsafe.Pointer
	err = capError("CreateForWindow", capCall(f, 3, hwnd, uintptr(unsafe.Pointer(&capIIDGraphicsCaptureItem)), uintptr(unsafe.Pointer(&p))))
	return p, err
}
func capReadFrame(dev, ctx, frame unsafe.Pointer, size capSize, opt captureOptions, staging *unsafe.Pointer, stagingSize *capSize) ([]byte, error) {
	var surface unsafe.Pointer
	if err := capError("frame Surface", capCall(frame, 6, uintptr(unsafe.Pointer(&surface)))); err != nil {
		return nil, err
	}
	defer capRelease(surface)
	var access unsafe.Pointer
	if err := capQI(surface, &capIIDSurfaceAccess, &access); err != nil {
		return nil, err
	}
	defer capRelease(access)
	var texture unsafe.Pointer
	if err := capError("surface GetInterface", capCall(access, 3, uintptr(unsafe.Pointer(&capIIDTexture2D)), uintptr(unsafe.Pointer(&texture)))); err != nil {
		return nil, err
	}
	defer capRelease(texture)
	var desc capTextureDesc
	capCall(texture, 10, uintptr(unsafe.Pointer(&desc)))
	texSize := capSize{int32(desc.Width), int32(desc.Height)}
	if !capValidSize(texSize) || size.Width > texSize.Width || size.Height > texSize.Height || desc.Format != dxgiFormatBGRA8 || desc.SampleCount != 1 {
		return nil, fmt.Errorf("capture texture dimensions or format are invalid")
	}
	if *staging == nil || *stagingSize != texSize {
		capRelease(*staging)
		*staging = nil
		desc.MipLevels, desc.ArraySize = 1, 1
		desc.Usage, desc.BindFlags, desc.CPUAccessFlags, desc.MiscFlags = 3, 0, 0x20000, 0
		if err := capError("CreateTexture2D staging", capCall(dev, 5, uintptr(unsafe.Pointer(&desc)), 0, uintptr(unsafe.Pointer(staging)))); err != nil {
			return nil, err
		}
		*stagingSize = texSize
	}
	// GPU readback scales with the source. CPU sampling and allocation scale
	// only with the configured preview, never a full-size CPU image copy.
	capCall(ctx, 47, uintptr(*staging), uintptr(texture))
	var mapped capMapped
	if err := capError("Map capture texture", capCall(ctx, 14, uintptr(*staging), 0, 1, 0, uintptr(unsafe.Pointer(&mapped)))); err != nil {
		return nil, err
	}
	defer capCall(ctx, 15, uintptr(*staging), 0)
	if mapped.Data == nil || uint64(mapped.RowPitch) < uint64(desc.Width)*4 {
		return nil, fmt.Errorf("capture texture has invalid row pitch")
	}
	return capSampleBGRA(mapped.Data, int(mapped.RowPitch), int(size.Width), int(size.Height), opt.width, opt.height), nil
}
func capSampleBGRA(src unsafe.Pointer, pitch, w, h, dw, dh int) []byte {
	out := make([]byte, dw*dh*4)
	for i := 3; i < len(out); i += 4 {
		out[i] = 255
	}
	x, y, sw, sh := capAspectFit(w, h, dw, dh)
	for j := 0; j < sh; j++ {
		row := unsafe.Slice((*byte)(unsafe.Add(src, j*h/sh*pitch)), w*4)
		for i := 0; i < sw; i++ {
			di, si := ((y+j)*dw+x+i)*4, i*w/sw*4
			copy(out[di:di+3], row[si:si+3])
		}
	}
	return out
}
func capAspectFit(srcW, srcH, dstW, dstH int) (x, y, w, h int) {
	if srcW < 1 || srcH < 1 || dstW < 1 || dstH < 1 {
		return
	}
	if dstW*srcH <= dstH*srcW {
		w, h = dstW, max(1, dstW*srcH/srcW)
	} else {
		w, h = max(1, dstH*srcW/srcH), dstH
	}
	return (dstW - w) / 2, (dstH - h) / 2, w, h
}
func capFrameDeadline(fps int) time.Duration {
	if fps < 1 {
		fps = 1
	}
	return time.Second / time.Duration(fps)
}
