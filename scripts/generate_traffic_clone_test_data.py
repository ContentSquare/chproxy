#!/usr/bin/env python3
"""Generate and verify ClickHouse data for chproxy traffic cloning tests."""

import argparse
import base64
import datetime as dt
import json
import os
import random
import re
import sys
import time
import urllib.error
import urllib.parse
import urllib.request


COUNTRIES = ("US", "UA", "DE", "FR", "GB", "PL", "ES", "BR")
DEVICES = ("desktop", "mobile", "tablet", "tv")
PAGES = ("/", "/pricing", "/docs", "/signup", "/dashboard", "/reports")
IDENT_RE = re.compile(r"^[A-Za-z_][A-Za-z0-9_]*$")


class ClickHouseHTTP:
    def __init__(self, base_url, user="", password="", timeout=30.0):
        self.base_url = normalize_url(base_url)
        self.user = user
        self.password = password
        self.timeout = timeout

    def execute(self, query, body=b"", query_id=None):
        params = {"query": query}
        if query_id:
            params["query_id"] = query_id
        url = add_query_params(self.base_url, params)
        headers = {"Content-Type": "text/plain; charset=utf-8"}
        if self.user:
            token = f"{self.user}:{self.password}".encode("utf-8")
            headers["Authorization"] = "Basic " + base64.b64encode(token).decode("ascii")

        request = urllib.request.Request(url, data=body, headers=headers, method="POST")
        try:
            with urllib.request.urlopen(request, timeout=self.timeout) as response:
                return response.read().decode("utf-8", errors="replace")
        except urllib.error.HTTPError as exc:
            detail = exc.read().decode("utf-8", errors="replace")
            raise RuntimeError(f"{self.base_url} returned HTTP {exc.code}: {detail}") from exc
        except urllib.error.URLError as exc:
            raise RuntimeError(f"cannot reach {self.base_url}: {exc.reason}") from exc


def normalize_url(url):
    if "://" not in url:
        url = "http://" + url
    parts = urllib.parse.urlsplit(url)
    path = parts.path or "/"
    return urllib.parse.urlunsplit((parts.scheme, parts.netloc, path, parts.query, parts.fragment))


def add_query_params(url, params):
    parts = urllib.parse.urlsplit(url)
    current = urllib.parse.parse_qsl(parts.query, keep_blank_values=True)
    current.extend(params.items())
    query = urllib.parse.urlencode(current)
    return urllib.parse.urlunsplit((parts.scheme, parts.netloc, parts.path, query, parts.fragment))


def checked_identifier(value):
    if not IDENT_RE.match(value):
        raise argparse.ArgumentTypeError(
            f"{value!r} is not a safe ClickHouse identifier; use letters, digits and underscore"
        )
    return value


def sql_string(value):
    return "'" + value.replace("\\", "\\\\").replace("'", "\\'") + "'"


def split_env_list(name):
    raw = os.getenv(name, "")
    return [item.strip() for item in raw.split(",") if item.strip()]


def make_batch_id():
    now = dt.datetime.now(dt.timezone.utc).strftime("%Y%m%dT%H%M%SZ")
    suffix = random.SystemRandom().randrange(100000, 999999)
    return f"tc_{now}_{suffix}"


def create_schema(client, database, table, reset=False):
    db = checked_identifier(database)
    tbl = checked_identifier(table)
    client.execute(f"CREATE DATABASE IF NOT EXISTS {db}")
    client.execute(
        f"""
CREATE TABLE IF NOT EXISTS {db}.{tbl}
(
    batch_id String,
    row_id UInt64,
    event_time DateTime64(3, 'UTC'),
    user_id UInt64,
    campaign_id UInt32,
    site_id UInt32,
    country LowCardinality(String),
    device LowCardinality(String),
    page String,
    revenue Decimal(18, 6),
    payload String
)
ENGINE = MergeTree
PARTITION BY toYYYYMM(event_time)
ORDER BY (batch_id, row_id)
""".strip()
    )
    if reset:
        client.execute(f"TRUNCATE TABLE IF EXISTS {db}.{tbl}")


def iter_rows(batch_id, start, count, seed, payload_bytes):
    rng = random.Random(seed + start)
    base = dt.datetime.now(dt.timezone.utc).replace(microsecond=0) - dt.timedelta(minutes=count)
    for offset in range(count):
        row_id = start + offset
        event_time = base + dt.timedelta(seconds=row_id)
        pad = "x" * max(0, payload_bytes - 32)
        yield {
            "batch_id": batch_id,
            "row_id": row_id,
            "event_time": event_time.strftime("%Y-%m-%d %H:%M:%S.%f")[:-3],
            "user_id": rng.randrange(1, 2_000_000),
            "campaign_id": rng.randrange(1, 500),
            "site_id": rng.randrange(1, 100),
            "country": rng.choice(COUNTRIES),
            "device": rng.choice(DEVICES),
            "page": rng.choice(PAGES),
            "revenue": f"{rng.randrange(0, 2_000_000) / 1_000_000:.6f}",
            "payload": f"{batch_id}:{row_id}:{pad}",
        }


def make_json_each_row(rows):
    lines = (json.dumps(row, separators=(",", ":")) for row in rows)
    return ("\n".join(lines) + "\n").encode("utf-8")


def insert_data(client, database, table, batch_id, rows, batch_size, seed, payload_bytes, sleep_ms):
    db = checked_identifier(database)
    tbl = checked_identifier(table)
    inserted = 0
    while inserted < rows:
        size = min(batch_size, rows - inserted)
        body = make_json_each_row(iter_rows(batch_id, inserted, size, seed, payload_bytes))
        query_id = f"{batch_id}_batch_{inserted // batch_size:05d}"
        client.execute(f"INSERT INTO {db}.{tbl} FORMAT JSONEachRow", body=body, query_id=query_id)
        inserted += size
        print(f"inserted {inserted}/{rows} rows via chproxy", flush=True)
        if sleep_ms:
            time.sleep(sleep_ms / 1000.0)


def read_stats(client, database, table, batch_id):
    db = checked_identifier(database)
    tbl = checked_identifier(table)
    query = f"""
SELECT
    count() AS rows,
    uniqExact(row_id) AS unique_rows,
    if(count() = 0, 0, min(row_id)) AS min_row_id,
    if(count() = 0, 0, max(row_id)) AS max_row_id,
    sum(row_id) AS row_id_sum,
    round(sum(toFloat64(revenue)), 6) AS revenue_sum
FROM {db}.{tbl}
WHERE batch_id = {sql_string(batch_id)}
FORMAT JSON
""".strip()
    raw = client.execute(query)
    parsed = json.loads(raw)
    data = parsed.get("data") or [{}]
    return normalize_stats(data[0])


def normalize_stats(stats):
    normalized = {}
    for key, value in stats.items():
        if isinstance(value, str) and value.isdigit():
            normalized[key] = int(value)
        else:
            normalized[key] = value
    return normalized


def fetch_metrics(metrics_url, timeout):
    url = normalize_url(metrics_url)
    try:
        with urllib.request.urlopen(url, timeout=timeout) as response:
            text = response.read().decode("utf-8", errors="replace")
    except urllib.error.URLError as exc:
        raise RuntimeError(f"cannot fetch metrics from {url}: {exc.reason}") from exc
    return [line for line in text.splitlines() if "traffic_clone_" in line and not line.startswith("#")]


def endpoint_label(kind, url, index=None):
    suffix = "" if index is None else f" #{index}"
    return f"{kind}{suffix} ({normalize_url(url)})"


def positive_int(value):
    parsed = int(value)
    if parsed <= 0:
        raise argparse.ArgumentTypeError("must be positive")
    return parsed


def non_negative_int(value):
    parsed = int(value)
    if parsed < 0:
        raise argparse.ArgumentTypeError("must be non-negative")
    return parsed


def percent(value):
    parsed = float(value)
    if parsed < 0 or parsed > 100:
        raise argparse.ArgumentTypeError("must be between 0 and 100")
    return parsed


def build_parser():
    parser = argparse.ArgumentParser(
        description=(
            "Create ClickHouse test tables, insert marked rows through chproxy, "
            "and verify that traffic-cloned targets received the same batch."
        )
    )
    parser.add_argument("--chproxy-url", default=os.getenv("CHPROXY_URL", "http://127.0.0.1:9001/"))
    parser.add_argument("--chproxy-user", default=os.getenv("CHPROXY_USER", "default"))
    parser.add_argument("--chproxy-password", default=os.getenv("CHPROXY_PASSWORD", "password"))
    parser.add_argument(
        "--primary-url",
        default=os.getenv("CLICKHOUSE_PRIMARY_URL"),
        help="optional direct ClickHouse HTTP URL for verifying the primary/source cluster",
    )
    parser.add_argument(
        "--clone-url",
        action="append",
        default=None,
        help="direct ClickHouse HTTP URL for a clone target; may be passed multiple times",
    )
    parser.add_argument("--clickhouse-user", default=os.getenv("CLICKHOUSE_USER", "default"))
    parser.add_argument("--clickhouse-password", default=os.getenv("CLICKHOUSE_PASSWORD", ""))
    parser.add_argument("--database", type=checked_identifier, default=os.getenv("TC_DATABASE", "traffic_clone_test"))
    parser.add_argument("--table", type=checked_identifier, default=os.getenv("TC_TABLE", "events"))
    parser.add_argument("--rows", type=positive_int, default=int(os.getenv("TC_ROWS", "10000")))
    parser.add_argument("--batch-size", type=positive_int, default=int(os.getenv("TC_BATCH_SIZE", "1000")))
    parser.add_argument("--batch-id", default=os.getenv("TC_BATCH_ID") or make_batch_id())
    parser.add_argument("--seed", type=int, default=int(os.getenv("TC_SEED", "42")))
    parser.add_argument("--payload-bytes", type=non_negative_int, default=int(os.getenv("TC_PAYLOAD_BYTES", "64")))
    parser.add_argument("--insert-sleep-ms", type=non_negative_int, default=int(os.getenv("TC_INSERT_SLEEP_MS", "0")))
    parser.add_argument("--clone-wait-seconds", type=float, default=float(os.getenv("TC_CLONE_WAIT_SECONDS", "2")))
    parser.add_argument("--timeout", type=float, default=float(os.getenv("TC_HTTP_TIMEOUT", "30")))
    parser.add_argument("--reset", action="store_true", help="truncate the test table on all endpoints before inserting")
    parser.add_argument(
        "--setup-via-chproxy",
        action="store_true",
        help=(
            "create/truncate the test table through chproxy, so chproxy credentials and "
            "traffic cloning create it in primary and clone targets"
        ),
    )
    parser.add_argument("--skip-setup", action="store_true")
    parser.add_argument("--skip-insert", action="store_true")
    parser.add_argument("--skip-verify", action="store_true")
    parser.add_argument(
        "--verify-primary-via-chproxy",
        action="store_true",
        help="verify primary row count through chproxy when no direct primary ClickHouse URL is available",
    )
    parser.add_argument(
        "--expect-clone-percent",
        type=percent,
        default=float(os.getenv("TC_EXPECT_CLONE_PERCENT", "100")),
        help="expected clone percentage; sampling is per INSERT request, not per row",
    )
    parser.add_argument("--clone-tolerance-rows", type=non_negative_int, default=0)
    parser.add_argument("--skip-clone-count-check", action="store_true")
    parser.add_argument("--non-strict", action="store_true", help="print verification failures but exit with code 0")
    parser.add_argument("--show-metrics", action="store_true")
    parser.add_argument("--metrics-url", default=os.getenv("CHPROXY_METRICS_URL"))
    return parser


def main():
    args = build_parser().parse_args()
    clone_urls = args.clone_url if args.clone_url is not None else split_env_list("CLICKHOUSE_CLONE_URLS")
    direct_endpoints = []
    if args.primary_url:
        direct_endpoints.append(("primary", args.primary_url))
    direct_endpoints.extend((f"clone #{idx}", url) for idx, url in enumerate(clone_urls, 1))

    chproxy = ClickHouseHTTP(args.chproxy_url, args.chproxy_user, args.chproxy_password, args.timeout)
    direct_clients = [
        (label, ClickHouseHTTP(url, args.clickhouse_user, args.clickhouse_password, args.timeout))
        for label, url in direct_endpoints
    ]
    if args.verify_primary_via_chproxy and not args.primary_url:
        direct_clients.insert(0, ("primary via chproxy", chproxy))

    print(f"batch_id: {args.batch_id}")
    print(f"table: {args.database}.{args.table}")
    print(f"chproxy: {normalize_url(args.chproxy_url)}")
    if args.primary_url:
        print(endpoint_label("primary", args.primary_url))
    elif args.verify_primary_via_chproxy:
        print("primary: will be verified through chproxy")
    else:
        print("primary: no --primary-url provided; primary direct verification will be skipped")
    if clone_urls:
        for idx, url in enumerate(clone_urls, 1):
            print(endpoint_label("clone", url, idx))
    else:
        print("clone: no --clone-url provided; only primary will be verified")

    if not args.skip_setup:
        if args.setup_via_chproxy:
            print(f"setting up through chproxy: {chproxy.base_url}")
            create_schema(chproxy, args.database, args.table, reset=args.reset)
        else:
            if not direct_clients:
                raise RuntimeError("setup requires --primary-url/--clone-url or --setup-via-chproxy")
            for label, client in direct_clients:
                if label == "primary via chproxy":
                    continue
                print(f"setting up {label}: {client.base_url}")
                create_schema(client, args.database, args.table, reset=args.reset)

    if not args.skip_insert:
        insert_data(
            chproxy,
            args.database,
            args.table,
            args.batch_id,
            args.rows,
            args.batch_size,
            args.seed,
            args.payload_bytes,
            args.insert_sleep_ms,
        )

    if args.clone_wait_seconds > 0 and clone_urls and not args.skip_insert:
        print(f"waiting {args.clone_wait_seconds:g}s for async clone requests")
        time.sleep(args.clone_wait_seconds)

    failures = []
    if not args.skip_verify:
        for label, client in direct_clients:
            stats = read_stats(client, args.database, args.table, args.batch_id)
            rows = int(stats.get("rows", 0))
            print(f"{label}: {json.dumps(stats, sort_keys=True)}")
            if label.startswith("primary"):
                if rows != args.rows:
                    failures.append(f"primary has {rows} rows, expected {args.rows}")
            elif not args.skip_clone_count_check:
                expected = int(round(args.rows * args.expect_clone_percent / 100.0))
                delta = abs(rows - expected)
                if delta > args.clone_tolerance_rows:
                    failures.append(
                        f"{label} has {rows} rows, expected {expected} +/- {args.clone_tolerance_rows}"
                    )

    if args.show_metrics:
        metrics_url = args.metrics_url or urllib.parse.urljoin(normalize_url(args.chproxy_url), "metrics")
        print(f"traffic clone metrics from {metrics_url}:")
        for line in fetch_metrics(metrics_url, args.timeout):
            print(line)

    if failures:
        print("verification failed:", file=sys.stderr)
        for failure in failures:
            print(f"  - {failure}", file=sys.stderr)
        return 0 if args.non_strict else 1

    print("done")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
