# Running Wera on a home server

The public site is the dashboard on Vercel. Vercel sends `/api` through a
Cloudflare quick tunnel to `wera-api-public` on this machine, so nothing
listens on the router. People on the LAN use `http://<server>:3000` and do not
touch the tunnel or the internet.

```
browser ──> Vercel ──> *.trycloudflare.com ──> wera-tunnel ──> wera-api-public
LAN     ──> :3000 wera-web ──> wera-api
```

## When the network drops

| Offline for | What happens to the tunnel | What brings it back |
|---|---|---|
| Up to 6 minutes or so | cloudflared reconnects on the same address | Nothing to do |
| More than about 12 minutes | Cloudflare forgets it. cloudflared keeps running and logs `control stream encountered a failure` until it is replaced | The tunnel watch |

Restarting the machine or `wera-tunnel` also gives a new address, and the watch
handles that the same way.

`tunnel-watch.sh` runs from cron every minute. While the site answers it does
nothing else. When the site stops answering:

1. Offline: it waits. A new tunnel needs the internet, and the old one may recover.
2. `wera-api-public` unhealthy: it waits. That is not the tunnel's fault.
3. `wera-tunnel` stopped: it waits. Docker restarts a tunnel that crashed, so a stopped one was stopped on purpose.
4. The tunnel answers but the site does not: Vercel has an old address. It commits the current one to `vercel.json` on `main`, which Vercel deploys, and on `dev`, so the next merge does not bring the old one back.
5. The tunnel does not answer: it restarts `wera-tunnel`, at once if cloudflared shows the error above, otherwise after 7 failed checks, then does step 4.

With the network back, the site returns in about 3 minutes, most of it Vercel's
deploy. A tab left open picks up by itself.

Those commits, `chore(deploy): point Vercel rewrites at the new tunnel URL`, are
expected. They cut no release.

## Install

```bash
./deploy/home/install-tunnel-watch.sh
```

Run it again after changing the script, since cron runs an installed copy so that
switching branches cannot break it. It adds a cron line and a deploy key that can
write to the frontend repository and nothing else. A desktop keyring is not used
because it stays locked until someone logs in, and this has to work after a power
cut with nobody at the machine. The deploy key is at
`~/.config/wera/tunnel-deploy-key`.

```bash
tail ~/.local/state/wera-tunnel/watch.log     # what it did and when
touch ~/.local/state/wera-tunnel/paused       # turn it off (rm to turn it on)
bash deploy/home/tunnel-watch.test.sh         # its tests
```

A named tunnel on a real domain makes all of this unnecessary: its address never
changes and Cloudflare does not forget it.
