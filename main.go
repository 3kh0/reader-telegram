package main

import (
	"context"
	"database/sql"
	"log"
	"os"
	"time"

	"github.com/joho/godotenv"
	_ "github.com/lib/pq"
	tele "gopkg.in/telebot.v4"
)

var db *sql.DB
var botUsername string

func main() {
	godotenv.Load()
	if tok := os.Getenv("TELEGRAM_BOT_TOKEN"); tok == "" {
		log.Fatal("token?")
	} else if d, err := initDB(); err != nil {
		log.Fatal("db :( ", err)
	} else {
		db = d
		defer db.Close()
		b, err := tele.NewBot(tele.Settings{Token: tok, Poller: &tele.LongPoller{Timeout: 10 * time.Second}})
		if err != nil {
			log.Fatal(err)
		}
		botUsername = b.Me.Username
		b.Handle("/start", handleStart)
		b.Handle("/list", handleList)
		b.Handle("/inspect", handleInspect)
		b.Handle(tele.OnText, handleText)
		b.Handle(tele.OnCallback, callback)
		go startFetcher(b)
		log.Println("Bot is running...")
		b.Start()
	}
}

func initDB() (*sql.DB, error) {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		log.Fatal("where db")
	}
	c, err := sql.Open("postgres", dsn)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := c.PingContext(ctx); err != nil {
		return nil, err
	}
	return c, nil
}
