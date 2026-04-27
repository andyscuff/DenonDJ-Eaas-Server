package main

import (
	"fmt"
	"net"
)

var (
	overrideMusicRoot   string
	overrideNavidromeDB string
	overrideHostIP      string
)

func getMusicRoot() string {
	if overrideMusicRoot != "" {
		return overrideMusicRoot
	}
	return "/srv/music"
}

func getNavidromeDB() string {
	if overrideNavidromeDB != "" {
		return overrideNavidromeDB
	}
	return "/srv/navidrome/data/navidrome.db"
}

func getHostIP() string {
	if overrideHostIP != "" {
		return overrideHostIP
	}
	// Auto-detect local IP
	conn, err := net.Dial("udp", "8.8.8.8:80")
	if err != nil {
		return "127.0.0.1"
	}
	defer conn.Close()
	return conn.LocalAddr().(*net.UDPAddr).IP.String()
}

func getArtworkBaseURL() string {
	return fmt.Sprintf("http://%s:50020", getHostIP())
}
