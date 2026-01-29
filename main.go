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

func handleStart(c tele.Context) error {
	payload := c.Message().Payload
	if payload == "" {
		return c.Send("Hello! Send me an RSS feed URL to subscribe, or use /list to see your feeds!")
	}

	parts := strings.SplitN(payload, "_", 2)
	if len(parts) != 2 {
		return c.Send("Don't know what to do with that!")
	}
	action, idStr := parts[0], parts[1]

	var subID int64
	if _, err := fmt.Sscanf(idStr, "%d", &subID); err != nil {
		return c.Send("bad id")
	}

	var userID int64
	if err := db.QueryRow("SELECT user_id FROM subscriptions WHERE id = $1", subID).Scan(&userID); err != nil {
		return c.Send("Subscription not found.")
	}
	if userID != c.Sender().ID {
		return c.Send("Subscription not found.")
	}

	switch action {
	case "rm":
		return handleRemoveCmd(c, subID)
	case "rf":
		return handleRefreshCmd(c, subID)
	case "ps":
		return handlePauseCmd(c, subID)
	case "lt":
		return handleLatestCmd(c, subID)
	default:
		return c.Send("Unknown action.")
	}
}

func buildListContent(userID int64) (string, error) {
	rows, err := db.Query("SELECT id, feed_url, title, refresh_interval, last_refreshed, paused FROM subscriptions WHERE user_id = $1 ORDER BY created_at", userID)
	if err != nil {
		return "", err
	}
	defer rows.Close()

	var entries []string
	for rows.Next() {
		var id int64
		var u, t sql.NullString
		var refreshInterval sql.NullInt64
		var lastRefreshed sql.NullTime
		var paused bool
		if err := rows.Scan(&id, &u, &t, &refreshInterval, &lastRefreshed, &paused); err != nil {
			continue
		}
		name := u.String
		if t.Valid && t.String != "" {
			name = t.String
		}
		status := "🟢"
		pauseLabel := "Pause"
		if paused {
			status = "⏸️"
			pauseLabel = "Resume"
		}

		entry := fmt.Sprintf("%s <b>%s</b>\n%s\nRefresh: %s", status, name, u.String, interval(refreshInterval.Int64))
		if lastRefreshed.Valid {
			entry += fmt.Sprintf(" | Last: %s", lastRefreshed.Time.Format("Jan 2 15:04"))
		}

		links := fmt.Sprintf("└ <a href=\"https://t.me/%s?start=rm_%d\">Remove</a> • <a href=\"https://t.me/%s?start=rf_%d\">Refresh</a> • <a href=\"https://t.me/%s?start=ps_%d\">%s</a> • <a href=\"https://t.me/%s?start=lt_%d\">Latest</a>",
			botUsername, id, botUsername, id, botUsername, id, pauseLabel, botUsername, id)

		entries = append(entries, entry+"\n"+links)
	}
	if len(entries) == 0 {
		return "", nil
	}
	return fmt.Sprintf("📋 <b>Your Subscriptions</b>\n\n%s", strings.Join(entries, "\n\n")), nil
}

func listKeyboard() *tele.ReplyMarkup {
	menu := &tele.ReplyMarkup{}
	menu.Inline(menu.Row(menu.Data("🔄 Refresh", "refresh_list")))
	return menu
}

func handleList(c tele.Context) error {
	content, err := buildListContent(c.Sender().ID)
	if err != nil {
		log.Printf("cant fetch subs %v", err)
		return c.Send("cant fetch subs")
	}
	if content == "" {
		return c.Send("No subscriptions yet. Send me an RSS feed URL to subscribe.")
	}
	return c.Send(content, tele.ModeHTML, tele.NoPreview, listKeyboard())
}

func handleRemoveCmd(c tele.Context, subID int64) error {
	var title sql.NullString
	db.QueryRow("SELECT title FROM subscriptions WHERE id = $1", subID).Scan(&title)
	if _, err := db.Exec("DELETE FROM subscriptions WHERE id = $1", subID); err != nil {
		return c.Send("Failed to remove.")
	}
	name := "subscription"
	if title.Valid && title.String != "" {
		name = title.String
	}
	return c.Send(fmt.Sprintf("✅ Removed %s", name))
}

func handleRefreshCmd(c tele.Context, subID int64) error {
	var title sql.NullString
	var currentInterval int64
	if err := db.QueryRow("SELECT title, refresh_interval FROM subscriptions WHERE id = $1", subID).Scan(&title, &currentInterval); err != nil {
		return c.Send("Subscription not found.")
	}
	name := "this feed"
	if title.Valid && title.String != "" {
		name = title.String
	}

	rm := &tele.ReplyMarkup{}
	rm.Inline(
		rm.Row(
			rm.Data("10m", fmt.Sprintf("setrefresh:%d:600", subID)),
			rm.Data("30m", fmt.Sprintf("setrefresh:%d:1800", subID)),
			rm.Data("1h", fmt.Sprintf("setrefresh:%d:3600", subID)),
		),
		rm.Row(
			rm.Data("6h", fmt.Sprintf("setrefresh:%d:21600", subID)),
			rm.Data("1d", fmt.Sprintf("setrefresh:%d:86400", subID)),
			rm.Data("1w", fmt.Sprintf("setrefresh:%d:604800", subID)),
		),
	)
	msg := fmt.Sprintf("⏱ Set refresh interval for <b>%s</b>\n\nCurrently set to: %s", name, interval(currentInterval))
	return c.Send(msg, tele.ModeHTML, rm)
}

func handlePauseCmd(c tele.Context, subID int64) error {
	var paused bool
	var title sql.NullString
	db.QueryRow("SELECT paused, title FROM subscriptions WHERE id = $1", subID).Scan(&paused, &title)
	newPaused := !paused
	if _, err := db.Exec("UPDATE subscriptions SET paused = $1 WHERE id = $2", newPaused, subID); err != nil {
		return c.Send("Failed to update.")
	}
	name := "subscription"
	if title.Valid && title.String != "" {
		name = title.String
	}
	if newPaused {
		return c.Send(fmt.Sprintf("⏸️ Paused %s", name))
	}
	return c.Send(fmt.Sprintf("▶️ Resumed %s", name))
}

func handleLatestCmd(c tele.Context, subID int64) error {
	var feedURL string
	if err := db.QueryRow("SELECT feed_url FROM subscriptions WHERE id = $1", subID).Scan(&feedURL); err != nil {
		return c.Send("Subscription not found.")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	feed, err := gofeed.NewParser().ParseURLWithContext(feedURL, ctx)
	if err != nil {
		return c.Send("Failed to fetch feed.")
	}
	if len(feed.Items) == 0 {
		return c.Send("No items in feed.")
	}

	item := feed.Items[0]
	msg := fmt.Sprintf("<b>%s</b>\n\n%s", feed.Title, item.Title)
	if item.Link != "" {
		msg += fmt.Sprintf("\n\n<a href=\"%s\">Read more</a>", item.Link)
	}
	return c.Send(msg, tele.ModeHTML)
}

func interval(seconds int64) string {
	switch {
	case seconds >= 86400:
		return fmt.Sprintf("%dd", seconds/86400)
	case seconds >= 3600:
		return fmt.Sprintf("%dh", seconds/3600)
	default:
		return fmt.Sprintf("%dm", seconds/60)
	}
}

func callback(c tele.Context) error {
	data := strings.TrimPrefix(c.Callback().Data, "\f")
	if data == "refresh_list" {
		content, err := buildListContent(c.Sender().ID)
		if err != nil {
			return c.Respond(&tele.CallbackResponse{Text: "cant fetch"})
		}
		if content == "" {
			c.Edit("No subscriptions yet! Send me an RSS feed URL to add one.")
			return c.Respond()
		}
		c.Edit(content, tele.ModeHTML, tele.NoPreview, listKeyboard())
		return c.Respond(&tele.CallbackResponse{Text: "Refreshed!"})
	}

	parts := strings.SplitN(data, ":", 3)
	if len(parts) < 3 {
		return c.Respond(&tele.CallbackResponse{Text: "invalid"})
	}

	var subID, iv int64
	if _, err := fmt.Sscanf(parts[1], "%d", &subID); err != nil {
		return c.Respond(&tele.CallbackResponse{Text: "invalid id"})
	}
	if _, err := fmt.Sscanf(parts[2], "%d", &iv); err != nil {
		return c.Respond(&tele.CallbackResponse{Text: "invalid interval"})
	}

	var userID int64
	if err := db.QueryRow("SELECT user_id FROM subscriptions WHERE id = $1", subID).Scan(&userID); err != nil {
		return c.Respond(&tele.CallbackResponse{Text: "not found"})
	}
	if userID != c.Sender().ID {
		return c.Respond(&tele.CallbackResponse{Text: "not yours"})
	}

	if _, err := db.Exec("UPDATE subscriptions SET refresh_interval = $1 WHERE id = $2", iv, subID); err != nil {
		return c.Respond(&tele.CallbackResponse{Text: "cant update"})
	}
	c.Edit(fmt.Sprintf("✅ Updated to %s", interval(iv)))
	return c.Respond(&tele.CallbackResponse{Text: fmt.Sprintf("✅ Set to %s", interval(iv))})
}

func handleText(c tele.Context) error {
	t := strings.TrimSpace(c.Text())
	lines := strings.Fields(t)
	var urls []string
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if isValidURL(line) {
			urls = append(urls, line)
		}
	}
	if len(urls) == 0 {
		return c.Send("invalid input")
	}
	return addSubs(c, urls)
}

func isValidURL(s string) bool {
	u, err := url.Parse(s)
	return err == nil && (u.Scheme == "http" || u.Scheme == "https")
}

func addSubs(c tele.Context, urls []string) error {
	total := len(urls)
	msg, _ := c.Bot().Send(c.Recipient(), fmt.Sprintf("⏳ Checking 1/%d...", total))
	edit := func(s string) {
		if msg != nil {
			c.Bot().Edit(msg, s)
		}
	}

	parser := gofeed.NewParser()
	var added []string
	var failed []string

	for i, u := range urls {
		edit(fmt.Sprintf("⏳ Checking %d/%d...", i+1, total))

		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		f, err := parser.ParseURLWithContext(u, ctx)
		cancel()

		if err != nil {
			log.Printf("cant parse %s: %v", u, err)
			failed = append(failed, u)
			continue
		}
		if _, err = db.Exec("INSERT INTO subscriptions (user_id, feed_url, title) VALUES ($1, $2, $3) ON CONFLICT (user_id, feed_url) DO UPDATE SET title = $3", c.Sender().ID, u, f.Title); err != nil {
			log.Printf("cant save %v", err)
			failed = append(failed, u)
			continue
		}
		added = append(added, f.Title)
	}

	if len(added) == 0 {
		edit("❌ Failed to add any feeds:\n" + strings.Join(failed, "\n"))
		return nil
	}

	var result string
	if len(added) == 1 {
		result = fmt.Sprintf("✅ Added %s", added[0])
	} else {
		result = fmt.Sprintf("✅ Added %d feeds: %s", len(added), strings.Join(added, ", "))
	}
	if len(failed) > 0 {
		result += fmt.Sprintf("\n❌ %d failed:\n%s", len(failed), strings.Join(failed, "\n"))
	}
	edit(result)
	return nil
}
