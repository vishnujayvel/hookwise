#!/usr/bin/env python3
"""
Calendar feed helper for hookwise.

Fetches upcoming Google Calendar events via the Google Calendar API
and outputs them as JSON to stdout.

Usage:
    python3 calendar-feed.py --lookahead 60 --credentials /path/to/credentials.json
    python3 calendar-feed.py --setup --credentials /path/to/credentials.json

Setup:
    1. Create OAuth 2.0 credentials in Google Cloud Console
    2. Download the credentials JSON file
    3. Set the path in hookwise.yaml under feeds.calendar.credentialsPath
    4. Install dependencies: pip install google-auth google-auth-oauthlib google-api-python-client
    5. Or use: hookwise setup calendar (handles steps 2-4 automatically)

The first run will open a browser for OAuth consent. Subsequent runs use
the cached token at ~/.hookwise/calendar-token.json.

The --setup flag runs only the OAuth flow and validates the token, then exits.
"""

import argparse
import json
import os
import sys
from datetime import datetime, timedelta, timezone
from pathlib import Path

TOKEN_PATH = os.path.expanduser("~/.hookwise/calendar-token.json")


def main():
    parser = argparse.ArgumentParser(description="Fetch Google Calendar events")
    parser.add_argument(
        "--lookahead",
        type=int,
        default=60,
        help="Minutes to look ahead (default: 60)",
    )
    parser.add_argument(
        "--credentials",
        required=True,
        help="Path to Google OAuth credentials JSON file",
    )
    parser.add_argument(
        "--setup",
        action="store_true",
        help="Only run the OAuth flow and validate the token, then exit",
    )
    args = parser.parse_args()

    if not os.path.exists(args.credentials):
        print(
            json.dumps({"error": "credentials file not found", "events": []}),
            file=sys.stdout,
        )
        sys.exit(1)

    try:
        from google.oauth2.credentials import Credentials
        from google_auth_oauthlib.flow import InstalledAppFlow
        from google.auth.transport.requests import Request
        from googleapiclient.discovery import build
    except ImportError:
        print(
            json.dumps(
                {
                    "error": "missing dependencies — run: pip install google-auth google-auth-oauthlib google-api-python-client",
                    "events": [],
                }
            ),
            file=sys.stdout,
        )
        sys.exit(1)

    SCOPES = ["https://www.googleapis.com/auth/calendar.readonly"]

    creds = None
    if os.path.exists(TOKEN_PATH):
        # Load without scope restriction — token may have been granted with
        # full calendar scope (e.g., shared with Google Calendar MCP).
        # Both calendar and calendar.readonly work for read-only queries.
        creds = Credentials.from_authorized_user_file(TOKEN_PATH)
        # Verify loaded scopes are compatible (must include calendar or calendar.readonly)
        if creds and creds.scopes and not any(
            s.endswith("calendar") or s.endswith("calendar.readonly") for s in creds.scopes
        ):
            creds = None  # Incompatible scopes — re-trigger OAuth

    if not creds or not creds.valid:
        if creds and creds.expired and creds.refresh_token:
            creds.refresh(Request())
        else:
            flow = InstalledAppFlow.from_client_secrets_file(args.credentials, SCOPES)
            creds = flow.run_local_server(port=0)

        Path(TOKEN_PATH).parent.mkdir(parents=True, exist_ok=True)
        with open(TOKEN_PATH, "w") as token_file:
            token_file.write(creds.to_json())

    # --setup mode: validate token and exit without fetching events
    if args.setup:
        if os.path.exists(TOKEN_PATH):
            print("Calendar OAuth setup complete. Token saved to " + TOKEN_PATH)
            sys.exit(0)
        else:
            print(
                "Setup failed: token file was not created at " + TOKEN_PATH,
                file=sys.stderr,
            )
            sys.exit(1)

    service = build("calendar", "v3", credentials=creds)

    now = datetime.now(timezone.utc)
    time_min = now.isoformat()
    time_max = (now + timedelta(minutes=args.lookahead)).isoformat()

    result = (
        service.events()
        .list(
            calendarId="primary",
            timeMin=time_min,
            timeMax=time_max,
            singleEvents=True,
            orderBy="startTime",
        )
        .execute()
    )

    events = []
    for item in result.get("items", []):
        start = item["start"].get("dateTime", item["start"].get("date", ""))
        end = item["end"].get("dateTime", item["end"].get("date", ""))
        title = item.get("summary", "(No title)")

        is_current = False
        if start and end:
            start_dt = datetime.fromisoformat(start)
            end_dt = datetime.fromisoformat(end)
            if start_dt.tzinfo is None:
                start_dt = start_dt.replace(tzinfo=timezone.utc)
            if end_dt.tzinfo is None:
                end_dt = end_dt.replace(tzinfo=timezone.utc)
            is_current = start_dt <= now < end_dt

        events.append(
            {
                "title": title,
                "start": start,
                "end": end,
                "is_current": is_current,
            }
        )

    print(json.dumps({"events": events}))


if __name__ == "__main__":
    main()
