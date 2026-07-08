import os
from http.server import ThreadingHTTPServer, SimpleHTTPRequestHandler

PUBLIC_DIR = os.path.join(os.path.dirname(os.path.abspath(__file__)), "public")


class Handler(SimpleHTTPRequestHandler):
    def __init__(self, *args, **kwargs):
        super().__init__(*args, directory=PUBLIC_DIR, **kwargs)

    def log_message(self, format, *args):
        print("%s - %s" % (self.address_string(), format % args))


def main():
    port = int(os.environ.get("PORT", "8000"))
    server = ThreadingHTTPServer(("0.0.0.0", port), Handler)
    print(f"PowerNode landing page serving {PUBLIC_DIR} on 0.0.0.0:{port}")
    server.serve_forever()


if __name__ == "__main__":
    main()
