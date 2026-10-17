package store

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"os/exec"
	"strconv"
	"strings"
)

// V1Domain 是 v1（Swift 版）的偏好设置域。
const V1Domain = "io.github.hanlunwang.OpenToggle" // 0.5 时的名字与 bundle id（项目原名 OpenToggle）

// V1Data 是从 v1 偏好设置里取出的数据。
type V1Data struct {
	Desired  map[string]bool
	Hidden   []string
	Params   map[string]map[string]string
	Seeded   []string
	Language string
}

// ReadV1 用 `defaults export` 读取 v1 的偏好设置。域不存在时返回空数据。
func ReadV1(domain string) (V1Data, error) {
	out, err := exec.Command("/usr/bin/defaults", "export", domain, "-").Output()
	if err != nil {
		return V1Data{}, nil // 没装过 v1
	}
	return ParseV1Plist(out)
}

// ParseV1Plist 从 XML plist 中取出 v1 用到的几个键；其他键（窗口位置等）忽略，
// 也不要求能解析二进制或日期值。
func ParseV1Plist(data []byte) (V1Data, error) {
	root, err := decodePlist(data)
	if err != nil {
		return V1Data{}, err
	}
	dict, ok := root.(map[string]any)
	if !ok {
		return V1Data{}, fmt.Errorf("v1 preferences: root is not a dictionary")
	}
	v := V1Data{Desired: map[string]bool{}, Params: map[string]map[string]string{}}
	if m, ok := dict["OpenToggle.desiredStates"].(map[string]any); ok {
		for id, raw := range m {
			v.Desired[id] = truthyAny(raw)
		}
	}
	v.Hidden = stringsOf(dict["OpenToggle.disabledSwitches"])
	v.Seeded = stringsOf(dict["OpenToggle.seededScripts"])
	if m, ok := dict["OpenToggle.paramValues"].(map[string]any); ok {
		for id, raw := range m {
			pm, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			v.Params[id] = map[string]string{}
			for k, val := range pm {
				v.Params[id][k] = scalarString(val)
			}
		}
	}
	if s, ok := dict["OpenToggle.language"].(string); ok {
		v.Language = s
	}
	return v, nil
}

// ImportV1 把 v1 数据并入当前状态（只做一次）。已有的 v2 数据优先。
func (s *Store) ImportV1(v V1Data) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.st.V1Imported {
		return false, nil
	}
	for id, on := range v.Desired {
		if _, exists := s.st.Desired[id]; !exists {
			s.st.Desired[id] = on
		}
	}
	for _, id := range v.Hidden {
		s.st.Hidden[id] = true
	}
	for id, m := range v.Params {
		if s.st.Params[id] == nil {
			s.st.Params[id] = map[string]string{}
		}
		for k, val := range m {
			if _, exists := s.st.Params[id][k]; !exists {
				s.st.Params[id][k] = val
			}
		}
	}
	for _, f := range v.Seeded {
		s.st.Seeded[f] = true
	}
	// v1 的「跟随系统」存成 "system"；v2 用空值表示跟随系统，只接受 zh-Hans 与 en
	if (v.Language == "zh-Hans" || v.Language == "en") && s.st.Settings["language"] == "" {
		s.st.Settings["language"] = v.Language
	}
	s.st.V1Imported = true
	return true, s.saveLocked()
}

func truthyAny(v any) bool {
	switch x := v.(type) {
	case bool:
		return x
	case int64:
		return x != 0
	case float64:
		return x != 0
	case string:
		return x == "1" || strings.EqualFold(x, "true") || strings.EqualFold(x, "yes")
	}
	return false
}

func scalarString(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case bool:
		return strconv.FormatBool(x)
	case int64:
		return strconv.FormatInt(x, 10)
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64)
	}
	return fmt.Sprint(v)
}

func stringsOf(v any) []string {
	arr, _ := v.([]any)
	var out []string
	for _, x := range arr {
		if s, ok := x.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

// decodePlist 是只认 dict / array / string / integer / real / true / false 的
// 最小 XML plist 解码器；data、date 等值解码为 nil。
func decodePlist(data []byte) (any, error) {
	d := xml.NewDecoder(bytes.NewReader(data))
	d.Strict = false
	for {
		tok, err := d.Token()
		if err != nil {
			return nil, fmt.Errorf("plist: %w", err)
		}
		if se, ok := tok.(xml.StartElement); ok && se.Name.Local != "plist" {
			return decodeValue(d, se)
		}
	}
}

func decodeValue(d *xml.Decoder, se xml.StartElement) (any, error) {
	switch se.Name.Local {
	case "dict":
		m := map[string]any{}
		var key string
		for {
			tok, err := d.Token()
			if err != nil {
				return nil, err
			}
			switch t := tok.(type) {
			case xml.StartElement:
				if t.Name.Local == "key" {
					var k string
					if err := d.DecodeElement(&k, &t); err != nil {
						return nil, err
					}
					key = k
					continue
				}
				v, err := decodeValue(d, t)
				if err != nil {
					return nil, err
				}
				m[key] = v
			case xml.EndElement:
				return m, nil
			}
		}
	case "array":
		var arr []any
		for {
			tok, err := d.Token()
			if err != nil {
				return nil, err
			}
			switch t := tok.(type) {
			case xml.StartElement:
				v, err := decodeValue(d, t)
				if err != nil {
					return nil, err
				}
				arr = append(arr, v)
			case xml.EndElement:
				return arr, nil
			}
		}
	case "true", "false":
		if err := d.Skip(); err != nil {
			return nil, err
		}
		return se.Name.Local == "true", nil
	}
	var text string
	if err := d.DecodeElement(&text, &se); err != nil && err != io.EOF {
		return nil, err
	}
	switch se.Name.Local {
	case "string":
		return text, nil
	case "integer":
		n, err := strconv.ParseInt(strings.TrimSpace(text), 10, 64)
		if err != nil {
			return nil, nil
		}
		return n, nil
	case "real":
		f, err := strconv.ParseFloat(strings.TrimSpace(text), 64)
		if err != nil {
			return nil, nil
		}
		return f, nil
	}
	return nil, nil
}
