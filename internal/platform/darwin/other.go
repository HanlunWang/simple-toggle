//go:build !darwin

package darwin

import (
	"errors"
	"os"
)

var errUnsupported = errors.New("not supported on this platform")

type PostResult int

const (
	Delivered PostResult = iota
	NotTrusted
	NotDelivered
	CreateFailed
)

type Fingerprint struct {
	Interface, Gateway, DHCPServer, Subnet, Domain string
	DNSDomains                                     []string
}

func (f Fingerprint) Key() string { return "" }

func AccessibilityTrusted() bool               { return false }
func RequestAccessibility() bool               { return false }
func PostKey(uint16, uint64) PostResult        { return NotTrusted }
func PostMouse(int) PostResult                 { return NotTrusted }
func IdleSeconds() (float64, bool)             { return 0, false }
func Trash(path string) error                  { return os.Remove(path) }
func WiFi() (string, string, error)            { return "", "", errUnsupported }
func LocationStatus() (int, error)             { return -1, errUnsupported }
func RequestLocation() error                   { return errUnsupported }
func NetworkFingerprint() (Fingerprint, error) { return Fingerprint{}, errUnsupported }
func PowerStatus() (Power, error)              { return Power{OnAC: true, Percent: 100}, errUnsupported }
func RunningApps() ([]App, error)              { return nil, errUnsupported }

func RevealWindow(uintptr, float64, float64, bool) {}
func FadeOutWindow(uintptr, float64)               {}
