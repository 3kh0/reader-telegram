package main

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/mmcdole/gofeed"
	tele "gopkg.in/telebot.v4"
)

const (
	fetchInterval   = 1 * time.Minute
	maxWorkers      = 5
	fetchTimeout    = 30 * time.Second
)

type Subscription struct {
	ID              int64
	UserID          int64
	FeedURL         string
	Title           string
	RefreshInterval int64
	LastRefreshed   sql.NullTime
	LastPostID      sql.NullString
	Paused          bool
}

func startFetcher(bot *tele.Bot) {
	ticker := time.NewTicker(fetchInterval)
	defer ticker.Stop()

	log.Println("Fetcher started")
	for range ticker.C {
		processDueFeeds(bot)
	}
}

func processDueFeeds(bot *tele.Bot) {
	subs, err := getDueSubscriptions()
	if err != nil {
		log.Printf("fetcher: cant get due subs: %v", err)
		return
	}
	if len(subs) == 0 {
		return
	}

	log.Printf("fetcher: processing %d due feeds", len(subs))

	jobs := make(chan Subscription, len(subs))
	var wg sync.WaitGroup

	workers := maxWorkers
	if len(subs) < workers {
		workers = len(subs)
	}

	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for sub := range jobs {
				processFeed(bot, sub)
			}
		}()
	}

	for _, sub := range subs {
		jobs <- sub
	}
	close(jobs)
	wg.Wait()
}

func getDueSubscriptions() ([]Subscription, error) {
	query := `
		SELECT id, user_id, feed_url, title, refresh_interval, last_refreshed, last_post_id, paused
		FROM subscriptions
		WHERE paused = FALSE
		  AND (last_refreshed IS NULL 
		       OR last_refreshed + (refresh_interval * interval '1 second') < NOW())
	`
	rows, err := db.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var subs []Subscription
	for rows.Next() {
		var s Subscription
		var title sql.NullString
		if err := rows.Scan(&s.ID, &s.UserID, &s.FeedURL, &title, &s.RefreshInterval, &s.LastRefreshed, &s.LastPostID, &s.Paused); err != nil {
			log.Printf("fetcher: scan error: %v", err)
			continue
		}
		if title.Valid {
			s.Title = title.String
		}
		subs = append(subs, s)
	}
	return subs, rows.Err()
}

func processFeed(bot *tele.Bot, sub Subscription) {
	ctx, cancel := context.WithTimeout(context.Background(), fetchTimeout)
	defer cancel()

	feed, err := gofeed.NewParser().ParseURLWithContext(sub.FeedURL, ctx)
	if err != nil {
		log.Printf("fetcher: cant parse %s: %v", sub.FeedURL, err)
		updateLastRefreshed(sub.ID)
		return
	}

	newItems := getNewItems(feed, sub.LastPostID.String)
	if len(newItems) == 0 {
		updateLastRefreshed(sub.ID)
		return
	}

	user := &tele.User{ID: sub.UserID}
	feedName := sub.Title
	if feedName == "" {
		feedName = feed.Title
	}

	for i := len(newItems) - 1; i >= 0; i-- {
		item := newItems[i]
		msg := formatItem(feedName, item)
		if _, err := bot.Send(user, msg, tele.ModeHTML); err != nil {
			log.Printf("fetcher: cant send to %d: %v", sub.UserID, err)
		}
		time.Sleep(100 * time.Millisecond)
	}

	var latestID string
	if len(feed.Items) > 0 {
		latestID = getItemID(feed.Items[0])
	}
	updateSubscription(sub.ID, latestID)
}

func getNewItems(feed *gofeed.Feed, lastPostID string) []*gofeed.Item {
	if lastPostID == "" && len(feed.Items) > 0 {
		return feed.Items[:1]
	}

	var newItems []*gofeed.Item
	for _, item := range feed.Items {
		if getItemID(item) == lastPostID {
			break
		}
		newItems = append(newItems, item)
	}
	return newItems
}

func getItemID(item *gofeed.Item) string {
	if item.GUID != "" {
		return item.GUID
	}
	return item.Link
}

func formatItem(feedName string, item *gofeed.Item) string {
	title := item.Title
	if title == "" {
		title = "New post"
	}
	msg := fmt.Sprintf("<b>%s</b>\n\n%s", feedName, title)
	if item.Link != "" {
		msg += fmt.Sprintf("\n\n<a href=\"%s\">Read more</a>", item.Link)
	}
	return msg
}

func updateLastRefreshed(subID int64) {
	_, err := db.Exec("UPDATE subscriptions SET last_refreshed = NOW() WHERE id = $1", subID)
	if err != nil {
		log.Printf("fetcher: cant update last_refreshed for %d: %v", subID, err)
	}
}

func updateSubscription(subID int64, lastPostID string) {
	_, err := db.Exec("UPDATE subscriptions SET last_refreshed = NOW(), last_post_id = $1 WHERE id = $2", lastPostID, subID)
	if err != nil {
		log.Printf("fetcher: cant update sub %d: %v", subID, err)
	}
}
