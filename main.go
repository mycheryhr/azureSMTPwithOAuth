package main

import (
	"flag"
	"fmt"
	"log/slog"
	"net"
	"os"
	"path/filepath"

	"log"

	"github.com/kardianos/service"
)

// program implements service.Interface
type program struct{}

const version = "1.0.0"

var (
	logFile    *os.File
	config     *tConfig
	configFile string
	logger     *slog.Logger
	svcFlag    = flag.String("service", "", "Control the system service (start, stop, install, uninstall)")
)

func (p *program) Start(s service.Service) error {
	// Start should not block. Do the actual work async.
	go p.run()
	return nil
}

func (p *program) run() {
	ln, err := net.Listen("tcp", config.ListenAddr)
	if err != nil {
		log.Fatalf("Failed to listen: %v", err)
	}
	defer ln.Close()
	for {
		conn, err := ln.Accept()
		if err != nil {
			log.Printf("Accept error: %v", err)
			continue
		}
		go handleSMTPConnection(conn)
	}
}

func (p *program) Stop(s service.Service) error {
	// Stop should not block. Return with a few seconds.
	log.Println("Service is stopping...")
	return nil
}

func main() {
	configFile = filepath.Join(filepath.Dir(os.Args[0]), "config.yaml")
	if err := loadConfig(); err != nil {
		log.Fatalf("failed to load config: %v", err)
	}
	if err := slogSetup(); err != nil {
		log.Fatalf("failed to initialize logger: %v", err)
	}
	flagsProcess()

	logger.Info("azureSMTPwithOAuth (systems@work) Github: https://github.com/mmalcek/azureSMTPwithOAuth")
	logger.Info("Starting Service", "version", version)

	defer logFile.Close()

	prg := &program{}
	svcConfig := &service.Config{
		Name:        "azureSMTPwithOAuth",
		DisplayName: "azureSMTPwithOAuth",
		Description: "azureSMTPwithOAuth (systems@work) is a service that provides SMTP functionality with OAuth authentication through the Microsoft Graph API. https://github.com/mmalcek/azureSMTPwithOAuth",
	}

	s, err := service.New(prg, svcConfig)
	if err != nil {
		logger.Error("service.New failed", "err", err)
	}

	// If -service flag is set, control the service (install, start, stop, uninstall)
	if *svcFlag != "" {
		err := service.Control(s, *svcFlag)
		if err != nil {
			log.Fatal("service.Control error: ", err)
		}
		switch *svcFlag {
		case "install":
			fmt.Println("Service installed successfully")
		case "uninstall":
			fmt.Println("Service uninstalled successfully")
		case "start":
			fmt.Println("Service started successfully")
		case "stop":
			fmt.Println("Service stopped successfully")
		}
		return
	}

	err = s.Run()
	if err != nil {
		logger.Error("service.Run failed", "err", err)
		os.Exit(1)
	}
}
