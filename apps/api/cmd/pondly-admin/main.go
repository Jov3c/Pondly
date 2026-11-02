package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/Jov3c/Pondly/apps/api/internal/auth"
	"github.com/Jov3c/Pondly/apps/api/internal/store"
	"github.com/google/uuid"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	databaseURL := os.Getenv("PONDLY_DATABASE_URL")
	if databaseURL == "" {
		log.Fatal("PONDLY_DATABASE_URL is required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	db, err := store.Open(ctx, databaseURL)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()
	if err := store.VerifySchema(ctx, db.Pool); err != nil {
		log.Fatal(err)
	}

	switch os.Args[1] {
	case "create":
		fs := flag.NewFlagSet("create", flag.ExitOnError)
		username := fs.String("username", "", "admin username")
		_ = fs.Parse(os.Args[2:])
		if *username == "" {
			log.Fatal("--username is required")
		}
		passwordBytes, err := io.ReadAll(os.Stdin)
		if err != nil {
			log.Fatal(err)
		}
		password := strings.TrimRight(string(passwordBytes), "\r\n")
		if len(password) < 12 {
			log.Fatal("password must be at least 12 characters; pass it on stdin")
		}
		hash, err := auth.HashPassword(password)
		if err != nil {
			log.Fatal(err)
		}
		secret, err := auth.NewTOTPSecret()
		if err != nil {
			log.Fatal(err)
		}
		id, err := db.CreateAdminAccount(ctx, *username, hash, secret)
		if err != nil {
			log.Fatal(err)
		}
		fmt.Printf("admin created: %s (%s)\n", *username, id)
		fmt.Printf("TOTP secret (shown once): %s\n", secret)
		fmt.Printf("otpauth://totp/Pondly:%s?secret=%s&issuer=Pondly\n",
			url.QueryEscape(*username), url.QueryEscape(secret))
	case "allowlist":
		if len(os.Args) < 4 {
			log.Fatal("usage: pondly-admin allowlist add|remove <agent-uuid>")
		}
		agentID, err := uuid.Parse(os.Args[3])
		if err != nil {
			log.Fatal("invalid agent uuid")
		}
		switch os.Args[2] {
		case "add":
			if err := db.AddAllowlist(ctx, agentID, nil); err != nil {
				log.Fatal(err)
			}
			fmt.Printf("allowlisted %s\n", agentID)
		case "remove":
			if err := db.RemoveAllowlist(ctx, agentID); err != nil {
				log.Fatal(err)
			}
			fmt.Printf("removed %s\n", agentID)
		default:
			log.Fatal("usage: pondly-admin allowlist add|remove <agent-uuid>")
		}
	default:
		usage()
		os.Exit(2)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: pondly-admin create --username <name> (password on stdin)")
	fmt.Fprintln(os.Stderr, "       pondly-admin allowlist add|remove <agent-uuid>")
}
