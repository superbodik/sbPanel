# PowerNode landing page

Static marketing site for PowerNode. Zero dependencies — `app.py` is a
stdlib-only static file server, so it runs on the panel's own "Python: Website"
egg with no `requirements.txt` needed.

## Run locally

```bash
python app.py            # serves ./public on 0.0.0.0:8000
PORT=3000 python app.py  # or pick a port
```

## Deploy on PowerNode itself

1. Create a server using the **Python: Website** egg.
2. Upload everything in this folder (`app.py`, `requirements.txt`, `public/`)
   to the server's `/home/container`.
3. Set the `PORT` environment variable on the server to match whatever port
   the panel allocated for it — the egg's startup command runs
   `python3 ${START_FILE:-app.py}`, and `app.py` reads `PORT` from the
   environment.
4. Start the server.

That's it — no build step, no framework, no external calls.
