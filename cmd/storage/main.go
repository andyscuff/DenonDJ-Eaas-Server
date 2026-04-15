package main

import (
	"context"
	"crypto/rand"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/icedream/go-stagelinq/eaas"
	"github.com/icedream/go-stagelinq/eaas/proto/enginelibrary"
	"github.com/icedream/go-stagelinq/eaas/proto/networktrust"
	"google.golang.org/grpc"
)

const (
	appName        = "Cubi Music Server"
	appVersion     = "1.0.0"
	timeout        = 5 * time.Second
	rescanInterval = 1 * time.Hour
)

var cubiToken eaas.Token = eaas.Token{
	0x5e, 0xff, 0xae, 0x59, 0x12, 0x88, 0x29, 0x30,
	0xde, 0xad, 0xc0, 0xde, 0xc0, 0xff, 0xee, 0x00,
}

var (
	flagMusicDir    string
	flagNavidromeDB string
	flagHostIP      string
)

var hostname string

func init() {
	var err error
	hostname, err = os.Hostname()
	if err != nil {
		hostname = "music-server"
	}
}

func main() {
	flag.StringVar(&flagMusicDir, "music-dir", "/srv/music", "Path to music library root")
	flag.StringVar(&flagNavidromeDB, "navidrome-db", "", "Path to Navidrome database (optional)")
	flag.StringVar(&flagHostIP, "host-ip", "", "Host IP address for artwork URLs (autodetected if not set)")
	flag.Parse()

	// Set globals used by other files
	if flagMusicDir != "" {
		overrideMusicRoot = flagMusicDir
	}
	if flagNavidromeDB != "" {
		overrideNavidromeDB = flagNavidromeDB
	}
	if flagHostIP != "" {
		overrideHostIP = flagHostIP
	}

	var token [16]byte
	if _, err := rand.Read(token[:]); err != nil {
		panic(err)
	}

	if err := loadLibrary(getMusicRoot()); err != nil {
		log.Fatalf("Failed to load library: %v", err)
	}

	ctx := context.Background()
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	termCh := make(chan os.Signal, 1)
	hupCh := make(chan os.Signal, 1)
	signal.Notify(termCh, syscall.SIGTERM, os.Interrupt)
	signal.Notify(hupCh, syscall.SIGHUP)

	go func() {
		<-termCh
		cancel()
	}()

	go func() {
		ticker := time.NewTicker(rescanInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				log.Println("Periodic rescan starting...")
				if err := loadLibrary(getMusicRoot()); err != nil {
					log.Printf("Rescan failed: %v", err)
				}
			case <-hupCh:
				log.Println("SIGHUP received, rescanning...")
				if err := loadLibrary(getMusicRoot()); err != nil {
					log.Printf("Rescan failed: %v", err)
				}
			case <-ctx.Done():
				return
			}
		}
	}()

	var s http.Server
	grpcServer := grpc.NewServer()

	go func() {
		<-ctx.Done()
		grpcServer.Stop()
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), time.Second*5)
		defer shutdownCancel()
		s.Shutdown(shutdownCtx)
	}()

	grpcPort := eaas.DefaultEAASGRPCPort
	grpcListener, err := net.ListenTCP("tcp", &net.TCPAddr{
		Port: int(grpcPort),
	})
	if err != nil {
		panic(err)
	}

	enginelibrary.RegisterEngineLibraryServiceServer(grpcServer, &EngineLibraryServiceServer{})
	networktrust.RegisterNetworkTrustServiceServer(grpcServer, &NetworkTrustServiceServer{})

	go func() {
		log.Println("Listening on GRPC")
		_ = grpcServer.Serve(grpcListener)
	}()

	s.Addr = fmt.Sprintf(":%d", eaas.DefaultEAASHTTPPort)
	s.Handler = eaasHTTPHandler()
	go func() {
		log.Println("Listening on HTTP")
		_ = s.ListenAndServe()
	}()

	log.Println("Beacon starting")
	beacon, err := eaas.StartBeaconWithConfiguration(&eaas.BeaconConfiguration{
		Name:            hostname,
		SoftwareVersion: appVersion,
		GRPCPort:        grpcPort,
		Token:           cubiToken,
	})
	if err != nil {
		panic(err)
	}
	defer func() {
		log.Println("Beacon shutting down")
		beacon.Shutdown()
	}()

	log.Println("Running")
	<-ctx.Done()
}
