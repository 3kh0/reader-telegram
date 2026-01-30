# Reader for Telegram

I use Telegram for almost all of my communications, and it is extremely helpful to have all of my RSS feeds in once place! However, the existing bots are not fast at updating, so I decided to make my own! And while I am here, why not try a new language?

Want to try it out? Just head over to [@reader_for_tg_bot](https://t.me/reader_for_tg_bot) and start adding your feeds!

## Cool stuff

- Just paste in a feed and it's added. No sweat.
- Adjustable refresh rates (if some feeds update faster than others)
- Bot is just fast.
- Written in Go, so it is Google Approved™️

## Deployment

This was made to be used with Coolify, so please deploy it from the [`Dockerfile`](./Dockerfile).

You will also want to spin up a simple PostgreSQL database to store your data, the default configuration seen in [`docker-compose.yml`](./docker-compose.yml) should work fine.

Then copy over the [`.env.example`](./.env.example) to `.env` and fill the Telegram bot token and DB URL.
