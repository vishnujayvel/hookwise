#!/usr/bin/env python3
"""
Calendar feed helper for hookwise.

Fetches upcoming Google Calendar events via the Google Calendar API
and outputs them as JSON to stdout.

Usage:
    python3 calendar-feed.py --lookahead 60 --credentials /path/to/credentials.json

Setup:
    1. Create OAuth 2.0 credentials in Google Cloud Console
    2. Download the credentials JSON file
    3. Set the path in hookwise.yaml under feeds.calendar.credentialsPath
    4. Install dependencies: pip install google-auth google-auth-oauthlib google-api-python-client

The first run will open a browser for OAuth consent. Subsequent runs use
the cached token at ~/.hookwise/calendar-token.json.
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
        creds = Credentials.from_authorized_user_file(TOKEN_PATH, SCOPES)

    if not creds or not creds.valid:
        if creds and creds.expired and creds.refresh_token:
            creds.refresh(Request())
        else:
            flow = InstalledAppFlow.from_client_secrets_file(args.credentials, SCOPES)
            creds = flow.run_local_server(port=0)

        Path(TOKEN_PATH).parent.mkdir(parents=True, exist_ok=True)
        with open(TOKEN_PATH, "w") as token_file:
            token_file.write(creds.to_json())

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
