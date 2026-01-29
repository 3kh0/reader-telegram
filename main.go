package main

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/joho/godotenv"
	_ "github.com/lib/pq"
	"github.com/mmcdole/gofeed"
	tele "gopkg.in/telebot.v4"
)

var db *sql.DB

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
		b.Handle("/start", handleStart)
		b.Handle("/list", handleList)
		b.Handle(tele.OnText, handleText)
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

func handleStart(c tele.Context) error {
	return c.Send("hello world!")
}

func handleList(c tele.Context) error {
	rows, err := db.Query("SELECT feed_url, title FROM subscriptions WHERE user_id = $1 ORDER BY created_at", c.Sender().ID)
	if err != nil {
		log.Printf("cant fetch subs %v", err)
		return c.Send("cant fetch subs")
	}
	defer rows.Close()

	var subs []string
	for rows.Next() {
		var u, t sql.NullString
		if err := rows.Scan(&u, &t); err != nil {
			continue
		}
		if t.Valid && t.String != "" {
			subs = append(subs, fmt.Sprintf("• %s\n  %s", t.String, u.String))
		} else {
			subs = append(subs, fmt.Sprintf("• %s", u.String))
		}
	}
	if len(subs) == 0 {
		return c.Send("none")
	}
	return c.Send(fmt.Sprintf("subs:\n\n%s", strings.Join(subs, "\n\n")))
}

func handleText(c tele.Context) error {
	t := strings.TrimSpace(c.Text())
	if !isValidURL(t) {
		return c.Send("invalid input")
	}
	return addSub(c, t)
}

func isValidURL(s string) bool {
	u, err := url.Parse(s)
	return err == nil && (u.Scheme == "http" || u.Scheme == "https")
}

func addSub(c tele.Context, url string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	f, err := gofeed.NewParser().ParseURLWithContext(url, ctx)
	if err != nil {
		log.Printf("cant parse %s: %v", url, err)
		return c.Send("cant parse")
	}
	if _, err = db.Exec("INSERT INTO subscriptions (user_id, feed_url, title) VALUES ($1, $2, $3) ON CONFLICT (user_id, feed_url) DO UPDATE SET title = $3", c.Sender().ID, url, f.Title); err != nil {
		log.Printf("cant save %v", err)
		return c.Send("cant save")
	}
	return c.Send(fmt.Sprintf("saved %s", f.Title))
}
