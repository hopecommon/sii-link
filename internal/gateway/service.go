package gateway

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/beevik/etree"
	"github.com/hopecommon/sii-link/internal/securefile"
)

func installedConfig() string {
	home, _ := os.UserHomeDir()
	path := filepath.Join(home, "Library", "LaunchAgents", "dev.hopecommon.sii-link.plist")
	doc := etree.NewDocument()
	if doc.ReadFromFile(path) == nil {
		array := doc.FindElement("/plist/dict/array")
		if array != nil {
			args := array.SelectElements("string")
			for i, item := range args {
				if (item.Text() == "-config" || item.Text() == "--config") && i+1 < len(args) {
					return args[i+1].Text()
				}
			}
		}
	}
	return filepath.Join(home, ".config", "sii-link", "config.toml")
}

func escapeXML(s string) string {
	var b bytes.Buffer
	_ = xml.EscapeText(&b, []byte(s))
	return b.String()
}

func installService(dir, binary, config string) error {
	home, _ := os.UserHomeDir()
	if dir != defaultDir() {
		return fmt.Errorf("custom state directory requires explicit gateway run --state-dir %s", dir)
	}
	var path, contents string
	switch runtime.GOOS {
	case "darwin":
		path = filepath.Join(home, "Library", "LaunchAgents", "dev.hopecommon.sii-link.plist")
		contents = fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
<key>Label</key><string>dev.hopecommon.sii-link</string>
<key>ProgramArguments</key><array><string>%s</string><string>gateway</string><string>run</string><string>--config</string><string>%s</string></array>
<key>RunAtLoad</key><true/>
<key>KeepAlive</key><dict><key>SuccessfulExit</key><false/></dict>
<key>ThrottleInterval</key><integer>30</integer>
<key>ProcessType</key><string>Background</string>
<key>StandardOutPath</key><string>/dev/null</string><key>StandardErrorPath</key><string>/dev/null</string>
</dict></plist>
`, escapeXML(binary), escapeXML(config))
	case "linux":
		path = filepath.Join(home, ".config", "systemd", "user", "sii-link.service")
		if strings.ContainsAny(binary+config, "\r\n%") {
			return fmt.Errorf("unsupported systemd path")
		}
		contents = fmt.Sprintf("[Unit]\nDescription=SII Link gateway supervisor\n[Service]\nType=simple\nExecStart=%s gateway run --config %s\nRestart=on-failure\nRestartSec=30\nNoNewPrivileges=true\nUMask=0077\n[Install]\nWantedBy=default.target\n", strconv.Quote(binary), strconv.Quote(config))
	default:
		return fmt.Errorf("gateway service installation requires macOS or Linux")
	}
	if old, err := os.ReadFile(path); err == nil && string(old) != contents {
		backupDir := filepath.Join(dir, "backups")
		if err := os.MkdirAll(backupDir, 0o700); err != nil {
			return err
		}
		if err := securefile.Write(filepath.Join(backupDir, filepath.Base(path)+"."+time.Now().Format("20060102-150405.000000000")), old); err != nil {
			return err
		}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	if err := securefile.Write(path, []byte(contents)); err != nil {
		return err
	}
	if runtime.GOOS == "darwin" {
		domain := fmt.Sprintf("gui/%d", os.Getuid())
		// Retire the previous, independently supervised relay without deleting it.
		legacy := domain + "/dev.hopecommon.sii-mini-relay"
		_ = exec.Command("launchctl", "disable", legacy).Run()
		if exec.Command("launchctl", "print", legacy).Run() == nil {
			if err := exec.Command("launchctl", "bootout", legacy).Run(); err != nil {
				return err
			}
		}
		label := domain + "/dev.hopecommon.sii-link"
		if exec.Command("launchctl", "print", label).Run() == nil {
			if err := exec.Command("launchctl", "bootout", label).Run(); err != nil {
				return err
			}
		}
		if err := exec.Command("launchctl", "enable", label).Run(); err != nil {
			return err
		}
		return exec.Command("launchctl", "bootstrap", domain, path).Run()
	}
	for _, args := range [][]string{{"--user", "daemon-reload"}, {"--user", "enable", "sii-link.service"}, {"--user", "restart", "sii-link.service"}} {
		if err := exec.Command("systemctl", args...).Run(); err != nil {
			return err
		}
	}
	return nil
}
