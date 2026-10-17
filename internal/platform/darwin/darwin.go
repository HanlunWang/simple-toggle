//go:build darwin

// Package darwin 是引擎用到的 macOS 系统调用：辅助功能检测、按键与鼠标合成、
// 输入空闲时间、废纸篓、Wi-Fi 与定位、网络指纹。全部通过 purego 调用，不用 cgo，
// 也不依赖 mygo（界面层之外的代码都要能独立构建与测试）。
package darwin

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
	"unsafe"

	"github.com/ebitengine/purego"
	"github.com/ebitengine/purego/objc"
)

const (
	frameworks = "/System/Library/Frameworks/"
	rtld       = purego.RTLD_NOW | purego.RTLD_GLOBAL

	kCGHIDEventTap         = 0
	kCGEventSourceHIDState = 1
	kCGEventKeyDown        = 10
	kCGAnyInputEventType   = ^uint32(0)
)

var (
	loadOnce sync.Once
	loadErr  error

	axIsProcessTrustedWithOptions func(options uintptr) bool
	cgEventCreate                 func(source uintptr) uintptr
	cgEventGetLocation            func(event uintptr) point
	cgEventCreateKeyboardEvent    func(source uintptr, key uint16, down bool) uintptr
	cgEventCreateMouseEvent       func(source uintptr, typ uint32, at point, button uint32) uintptr
	cgEventSetFlags               func(event uintptr, flags uint64)
	cgEventPost                   func(tap uint32, event uintptr)
	cgSecondsSinceLastEventType   func(state int32, eventType uint32) float64
	cfRelease                     func(ref uintptr)
)

type point struct{ X, Y float64 }

func load() error {
	loadOnce.Do(func() {
		var as, cg, cf uintptr
		if as, loadErr = purego.Dlopen(frameworks+"ApplicationServices.framework/ApplicationServices", rtld); loadErr != nil {
			return
		}
		if cg, loadErr = purego.Dlopen(frameworks+"CoreGraphics.framework/CoreGraphics", rtld); loadErr != nil {
			return
		}
		if cf, loadErr = purego.Dlopen(frameworks+"CoreFoundation.framework/CoreFoundation", rtld); loadErr != nil {
			return
		}
		purego.RegisterLibFunc(&axIsProcessTrustedWithOptions, as, "AXIsProcessTrustedWithOptions")
		purego.RegisterLibFunc(&cgEventCreate, cg, "CGEventCreate")
		purego.RegisterLibFunc(&cgEventGetLocation, cg, "CGEventGetLocation")
		purego.RegisterLibFunc(&cgEventCreateKeyboardEvent, cg, "CGEventCreateKeyboardEvent")
		purego.RegisterLibFunc(&cgEventCreateMouseEvent, cg, "CGEventCreateMouseEvent")
		purego.RegisterLibFunc(&cgEventSetFlags, cg, "CGEventSetFlags")
		purego.RegisterLibFunc(&cgEventPost, cg, "CGEventPost")
		purego.RegisterLibFunc(&cgSecondsSinceLastEventType, cg, "CGEventSourceSecondsSinceLastEventType")
		purego.RegisterLibFunc(&cfRelease, cf, "CFRelease")
	})
	return loadErr
}

// AccessibilityTrusted 报告本进程是否已获辅助功能授权。不弹授权框。
func AccessibilityTrusted() bool {
	if load() != nil {
		return false
	}
	return axIsProcessTrustedWithOptions(0)
}

var (
	promptOnce sync.Once
	promptDict uintptr
)

// RequestAccessibility 弹出系统的辅助功能授权提示（并把 app 加进系统设置的列表），
// 返回当前是否已授权。只应由用户在 app 里主动触发；命令行进程不调用它。
func RequestAccessibility() bool {
	if load() != nil {
		return false
	}
	promptOnce.Do(func() {
		hi, err := purego.Dlopen(frameworks+"ApplicationServices.framework/Frameworks/HIServices.framework/HIServices", rtld)
		if err != nil {
			return
		}
		cf, err := purego.Dlopen(frameworks+"CoreFoundation.framework/CoreFoundation", rtld)
		if err != nil {
			return
		}
		keyPtr, err1 := purego.Dlsym(hi, "kAXTrustedCheckOptionPrompt")
		truePtr, err2 := purego.Dlsym(cf, "kCFBooleanTrue")
		keyCB, err3 := purego.Dlsym(cf, "kCFTypeDictionaryKeyCallBacks")
		valCB, err4 := purego.Dlsym(cf, "kCFTypeDictionaryValueCallBacks")
		if err1 != nil || err2 != nil || err3 != nil || err4 != nil {
			return
		}
		var create func(alloc uintptr, keys, values *uintptr, n int, kcb, vcb uintptr) uintptr
		purego.RegisterLibFunc(&create, cf, "CFDictionaryCreate")
		// 两个符号都是 C 全局变量（CFStringRef / CFBooleanRef），地址固定：读出变量里的指针值
		k := **(**uintptr)(unsafe.Pointer(&keyPtr))
		v := **(**uintptr)(unsafe.Pointer(&truePtr))
		promptDict = create(0, &k, &v, 1, keyCB, valCB)
	})
	if promptDict == 0 {
		return axIsProcessTrustedWithOptions(0)
	}
	return axIsProcessTrustedWithOptions(promptDict)
}

// PostResult 是一次合成的结果。CGEventPost 没有返回值，被系统丢弃时毫无声响，
// 所以用「距上一次该类输入的时间」是否归零来判断事件是否真的送达。
type PostResult int

const (
	Delivered PostResult = iota
	NotTrusted
	NotDelivered
	CreateFailed
)

// PostKey 合成一次按下与抬起，并验证送达。
func PostKey(code uint16, flags uint64) PostResult {
	if load() != nil || !axIsProcessTrustedWithOptions(0) {
		return NotTrusted
	}
	before := cgSecondsSinceLastEventType(kCGEventSourceHIDState, kCGEventKeyDown)
	for _, down := range []bool{true, false} {
		ev := cgEventCreateKeyboardEvent(0, code, down)
		if ev == 0 {
			return CreateFailed
		}
		cgEventSetFlags(ev, flags)
		cgEventPost(kCGHIDEventTap, ev)
		cfRelease(ev)
	}
	return verify(before, kCGEventKeyDown)
}

// PostMouse 在当前光标位置点击 button（0 左、1 右、2 中）。
func PostMouse(button int) PostResult {
	if load() != nil || !axIsProcessTrustedWithOptions(0) {
		return NotTrusted
	}
	// kCGEventLeftMouseDown=1/Up=2，RightMouseDown=3/Up=4，OtherMouseDown=25/Up=26
	down, up := uint32(1), uint32(2)
	switch button {
	case 1:
		down, up = 3, 4
	case 2:
		down, up = 25, 26
	}
	probe := cgEventCreate(0)
	if probe == 0 {
		return CreateFailed
	}
	at := cgEventGetLocation(probe)
	cfRelease(probe)
	before := cgSecondsSinceLastEventType(kCGEventSourceHIDState, down)
	for _, typ := range []uint32{down, up} {
		ev := cgEventCreateMouseEvent(0, typ, at, uint32(button))
		if ev == 0 {
			return CreateFailed
		}
		cgEventPost(kCGHIDEventTap, ev)
		cfRelease(ev)
	}
	return verify(before, down)
}

func verify(before float64, eventType uint32) PostResult {
	time.Sleep(50 * time.Millisecond)
	after := cgSecondsSinceLastEventType(kCGEventSourceHIDState, eventType)
	// 用户刚好也在操作（before 已接近 0）时无法区分，按送达处理
	if after < before || after < 0.5 {
		return Delivered
	}
	return NotDelivered
}

// IdleSeconds 是距上一次任何键盘或鼠标输入的秒数。
func IdleSeconds() (float64, bool) {
	if load() != nil {
		return 0, false
	}
	return cgSecondsSinceLastEventType(kCGEventSourceHIDState, kCGAnyInputEventType), true
}

// ───────── 废纸篓 ─────────

var (
	foundationOnce sync.Once
	foundationErr  error
)

func loadFoundation() error {
	foundationOnce.Do(func() {
		_, foundationErr = purego.Dlopen(frameworks+"Foundation.framework/Foundation", rtld)
	})
	return foundationErr
}

func nsString(s string) objc.ID {
	b := append([]byte(s), 0)
	return objc.ID(objc.GetClass("NSString")).Send(objc.RegisterName("stringWithUTF8String:"), &b[0])
}

// Trash 把文件移到废纸篓（与 Finder 的「移到废纸篓」相同，可以放回原处）。
func Trash(path string) error {
	abs, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	if _, err := os.Lstat(abs); err != nil {
		return err
	}
	if err := loadFoundation(); err == nil {
		url := objc.ID(objc.GetClass("NSURL")).Send(objc.RegisterName("fileURLWithPath:"), nsString(abs))
		fm := objc.ID(objc.GetClass("NSFileManager")).Send(objc.RegisterName("defaultManager"))
		ok := objc.Send[bool](fm, objc.RegisterName("trashItemAtURL:resultingItemURL:error:"), url, uintptr(0), uintptr(0))
		if ok {
			return nil
		}
	}
	// 兜底：直接移进 ~/.Trash，重名时加时间戳
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	dst := filepath.Join(home, ".Trash", filepath.Base(abs))
	if _, err := os.Lstat(dst); err == nil {
		dst += "." + time.Now().Format("150405.000")
	}
	return os.Rename(abs, dst)
}

// ───────── Wi-Fi 与定位 ─────────

var (
	wlanOnce sync.Once
	wlanErr  error
	locOnce  sync.Once
	locErr   error
	manager  objc.ID
)

func goString(s objc.ID) string {
	if s == 0 {
		return ""
	}
	p := objc.Send[*byte](s, objc.RegisterName("UTF8String"))
	if p == nil {
		return ""
	}
	n := 0
	for *(*byte)(unsafe.Add(unsafe.Pointer(p), n)) != 0 {
		n++
	}
	return string(unsafe.Slice(p, n))
}

// WiFi 返回 CoreWLAN 看到的接口名与 SSID。没有定位授权时 SSID 为空（macOS 14 起）。
func WiFi() (iface, ssid string, err error) {
	wlanOnce.Do(func() { _, wlanErr = purego.Dlopen(frameworks+"CoreWLAN.framework/CoreWLAN", rtld) })
	if wlanErr != nil {
		return "", "", wlanErr
	}
	client := objc.ID(objc.GetClass("CWWiFiClient")).Send(objc.RegisterName("sharedWiFiClient"))
	if client == 0 {
		return "", "", errors.New("CWWiFiClient unavailable")
	}
	in := client.Send(objc.RegisterName("interface"))
	if in == 0 {
		return "", "", nil
	}
	return goString(in.Send(objc.RegisterName("interfaceName"))), goString(in.Send(objc.RegisterName("ssid"))), nil
}

// LocationStatus 是 CLAuthorizationStatus：0 未决定，1 受限，2 拒绝，3 始终，4 使用期间。
// 必须在主线程调用。
func LocationStatus() (int, error) {
	locOnce.Do(func() { _, locErr = purego.Dlopen(frameworks+"CoreLocation.framework/CoreLocation", rtld) })
	if locErr != nil {
		return -1, locErr
	}
	if manager == 0 {
		manager = objc.ID(objc.GetClass("CLLocationManager")).Send(objc.RegisterName("new"))
	}
	return int(objc.Send[int32](manager, objc.RegisterName("authorizationStatus"))), nil
}

// RequestLocation 弹出系统定位授权框。必须在主线程、由用户主动触发时调用。
func RequestLocation() error {
	if _, err := LocationStatus(); err != nil {
		return err
	}
	manager.Send(objc.RegisterName("requestWhenInUseAuthorization"))
	return nil
}

// ───────── 网络指纹（无需任何权限） ─────────

// Fingerprint 用来识别「当前在哪个网络」，替代需要定位权限的 SSID。
type Fingerprint struct {
	Interface  string   `json:"interface"`
	Gateway    string   `json:"gateway"`
	DHCPServer string   `json:"dhcpServer"`
	Subnet     string   `json:"subnet"`
	Domain     string   `json:"domain"`
	DNSDomains []string `json:"dnsDomains"`
}

// Key 是用于规则匹配的稳定标识：同一路由器下不变，换网络即变。
func (f Fingerprint) Key() string {
	return strings.Join([]string{f.Gateway, f.DHCPServer, f.Subnet, f.Domain}, "|")
}

var (
	reRouteGateway = regexp.MustCompile(`(?m)^\s*gateway:\s*(\S+)`)
	reRouteIface   = regexp.MustCompile(`(?m)^\s*interface:\s*(\S+)`)
	reDHCPServer   = regexp.MustCompile(`(?m)^server_identifier \(ip\): (\S+)`)
	reDHCPSubnet   = regexp.MustCompile(`(?m)^subnet_mask \(ip\): (\S+)`)
	reDHCPDomain   = regexp.MustCompile(`(?m)^domain_name \(string\): (\S+)`)
	reDNSDomain    = regexp.MustCompile(`(?m)^\s*(?:search domain\[\d+\]|domain)\s*:\s*(\S+)`)
)

func submatch(re *regexp.Regexp, b []byte) string {
	if m := re.FindSubmatch(b); m != nil {
		return string(m[1])
	}
	return ""
}

// NetworkFingerprint 读取默认路由、该接口的 DHCP 报文与系统 DNS 搜索域。
// 没有默认路由（离线）时返回错误。
func NetworkFingerprint() (Fingerprint, error) {
	var fp Fingerprint
	out, err := exec.Command("/sbin/route", "-n", "get", "default").Output()
	if err != nil {
		return fp, err
	}
	fp.Gateway, fp.Interface = submatch(reRouteGateway, out), submatch(reRouteIface, out)
	if fp.Interface != "" {
		if pkt, err := exec.Command("/usr/sbin/ipconfig", "getpacket", fp.Interface).Output(); err == nil {
			fp.DHCPServer, fp.Subnet, fp.Domain = submatch(reDHCPServer, pkt), submatch(reDHCPSubnet, pkt), submatch(reDHCPDomain, pkt)
		}
	}
	if dns, err := exec.Command("/usr/sbin/scutil", "--dns").Output(); err == nil {
		seen := map[string]bool{}
		for _, m := range reDNSDomain.FindAllSubmatch(dns, -1) {
			d := strings.TrimSuffix(string(m[1]), ".")
			if !seen[d] && !strings.HasSuffix(d, ".arpa") && d != "local" {
				seen[d] = true
				fp.DNSDomains = append(fp.DNSDomains, d)
			}
		}
	}
	return fp, nil
}
