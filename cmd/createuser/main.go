package main

import (
	"context"
	"crypto/sha256"
	"fmt"
	"log"
	"os"

	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"
)

func main() {
	if len(os.Args) < 4 {
		fmt.Fprintln(os.Stderr, "usage: createuser <name> <email> <password>")
		os.Exit(1)
	}
	name, email, plainPwd := os.Args[1], os.Args[2], os.Args[3]

	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		log.Fatal("DATABASE_URL not set")
	}

	pool, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		log.Fatalf("connect: %v", err)
	}
	defer pool.Close()

	// Frontend pre-hashes with SHA-256 before sending, so we must match that.
	sum := sha256.Sum256([]byte(plainPwd))
	sha256hex := fmt.Sprintf("%x", sum)

	hashed, err := bcrypt.GenerateFromPassword([]byte(sha256hex), bcrypt.DefaultCost)
	if err != nil {
		log.Fatalf("bcrypt: %v", err)
	}

	var id string
	err = pool.QueryRow(context.Background(), `
		INSERT INTO users (name, email, role, password_hash, active)
		VALUES ($1, $2, 'ADMINISTRADOR', $3, true)
		ON CONFLICT (email) DO UPDATE
		  SET name=$1, password_hash=$3, active=true, updated_at=now()
		RETURNING id`,
		name, email, string(hashed),
	).Scan(&id)
	if err != nil {
		log.Fatalf("upsert user: %v", err)
	}

	_, err = pool.Exec(context.Background(), `
		INSERT INTO settings (user_id, key, value) VALUES
		  ($1, 'baseCurrency',     '"COP"'),
		  ($1, 'autoExchangeRate', 'true')
		ON CONFLICT (user_id, key) DO NOTHING`,
		id)
	if err != nil {
		log.Fatalf("seed settings: %v", err)
	}

	fmt.Printf("user upserted: id=%s email=%s\n", id, email)
}
