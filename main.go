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
	rows, err := db.Query("SELECT feed_url, title, refresh_interval, last_refreshed, last_post_id FROM subscriptions WHERE user_id = $1 ORDER BY created_at", c.Sender().ID)
	if err != nil {
		log.Printf("cant fetch subs %v", err)
		return c.Send("cant fetch subs")
	}
	defer rows.Close()

	var subs []string
	for rows.Next() {
		var u, t, lastPostID sql.NullString
		var refreshInterval sql.NullInt64
		var lastRefreshed sql.NullTime
		if err := rows.Scan(&u, &t, &refreshInterval, &lastRefreshed, &lastPostID); err != nil {
			continue
		}
		name := u.String
		if t.Valid && t.String != "" {
			name = t.String
		}
		entry := fmt.Sprintf("• %s\n  %s\n  refresh: %ds", name, u.String, refreshInterval.Int64)
		if lastRefreshed.Valid {
			entry += fmt.Sprintf(" | last: %s", lastRefreshed.Time.Format("2006-01-02 15:04"))
		} else {
			entry += " | last: never"
		}
		if lastPostID.Valid && lastPostID.String != "" {
			entry += fmt.Sprintf("\n  last post: %s", lastPostID.String)
		}
		subs = append(subs, entry)
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
