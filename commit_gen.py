import os
import random
import subprocess
from datetime import datetime, timedelta

COMMIT_MESSAGES = [
    "add middleware for request validation",
    "fix: resolve nil pointer dereference in handler",
    "refactor: extract common db query into helper",
    "update go.mod dependencies",
    "implement pagination for list endpoints",
    "fix: handle timeout errors in grpc client",
    "test: add unit tests for auth service",
    "docs: update API documentation",
    "refactor: simplify error handling in router",
    "add prometheus metrics endpoint",
    "fix: correct off-by-one in slice bounds",
    "clean up unused imports",
    "add context propagation to db calls",
    "fix: race condition in worker pool",
    "refactor: move config parsing to dedicated package",
    "test: add integration tests for user service",
    "implement rate limiting middleware",
    "fix: incorrect status code on validation error",
    "upgrade golangci-lint to v1.56",
    "add structured logging with zerolog",
    "fix: memory leak in connection pool",
    "refactor: replace global state with dependency injection",
    "test: mock external http calls in service tests",
    "add health check endpoint",
    "fix: deadlock in mutex usage",
    "add makefile targets for build and test",
    "implement graceful shutdown",
    "fix: panic on empty request body",
    "refactor: split large handler into smaller functions",
    "docs: add setup instructions to README",
]

GO_FILES = [
    "main.go",
    "handler/user.go",
    "handler/auth.go",
    "service/user.go",
    "service/auth.go",
    "middleware/logging.go",
    "middleware/auth.go",
    "db/postgres.go",
    "config/config.go",
    "utils/helpers.go",
]

GO_SNIPPETS = [
    'log.Info().Str("method", r.Method).Msg("request received")\n',
    'ctx, cancel := context.WithTimeout(ctx, 5*time.Second)\ndefer cancel()\n',
    '// TODO: add retry logic\n',
    'if err != nil {\n\treturn nil, fmt.Errorf("db query failed: %w", err)\n}\n',
    'metrics.RequestCount.WithLabelValues(route).Inc()\n',
    'cfg := config.Load()\n',
    'defer db.Close()\n',
    'wg.Add(1)\ngo func() {\n\tdefer wg.Done()\n}()\n',
    'slog.Info("starting server", "port", cfg.Port)\n',
    'rows, err := db.QueryContext(ctx, query, args...)\n',
]


def get_positive_int(prompt, default=20):
    while True:
        try:
            user_input = input(f"{prompt} (default {default}): ")
            if not user_input.strip():
                return default
            value = int(user_input)
            if value > 0:
                return value
            else:
                print("Please enter a positive integer.")
        except ValueError:
            print("Invalid input. Please enter a valid integer.")


def get_repo_path(prompt, default="."):
    while True:
        user_input = input(f"{prompt} (default current directory): ")
        if not user_input.strip():
            return default
        if os.path.isdir(user_input):
            return user_input
        else:
            print("Directory does not exist. Please enter a valid path.")


def get_year_selection():
    available = [2020, 2021, 2022, 2023, 2024, 2025, 2026]
    print("\nWhich year(s) to spread commits across?")
    for i, y in enumerate(available, 1):
        print(f"  {i}. {y}")
    print("  all. All years (2020–2026)")
    print("  Enter multiple numbers separated by commas, e.g. 1,3,5")
    while True:
        choice = input("Enter choice (default all): ").strip().lower()
        if not choice or choice == "all":
            return available
        try:
            indices = [int(x.strip()) for x in choice.split(",")]
            if all(1 <= i <= len(available) for i in indices):
                return [available[i - 1] for i in indices]
            print(f"Please enter numbers between 1 and {len(available)}.")
        except ValueError:
            print("Invalid input. Enter numbers like 1 or 1,3,5 or all.")


def build_commit_schedule(total_commits, years):
    """
    Groups commits into days so each active day gets 1–9 commits.
    Shade weights are skewed toward lighter greens (very light > light > medium)
    so the graph looks naturally distributed rather than uniformly dense.

      Very light green : 1–3 commits/day  (weight 3)
      Light green      : 4–6 commits/day  (weight 2)
      Medium green     : 7–9 commits/day  (weight 1)
    """
    all_dates = []
    for year in years:
        start = datetime(year, 1, 1).date()
        end = datetime(year, 12, 31).date()
        delta = (end - start).days
        for i in range(delta + 1):
            all_dates.append(start + timedelta(days=i))

    used_days = set()
    schedule = []
    remaining = total_commits

    while remaining > 0:
        if len(used_days) >= len(all_dates):
            day = random.choice(all_dates)
        else:
            day = random.choice(all_dates)
            while day in used_days:
                day = random.choice(all_dates)
            used_days.add(day)

        shade = random.choices(
            ["very_light", "light", "medium", "dark"],
            weights=[3, 3, 2, 1],
            k=1
        )[0]
        if shade == "very_light":
            day_count = random.randint(1, 3)
        elif shade == "light":
            day_count = random.randint(4, 6)
        elif shade == "medium":
            day_count = random.randint(7, 9)
        else:
            day_count = random.randint(10, 15)

        day_count = min(day_count, remaining)

        for _ in range(day_count):
            hour = random.randint(0, 23)
            minute = random.randint(0, 59)
            second = random.randint(0, 59)
            schedule.append(datetime(day.year, day.month, day.day, hour, minute, second))

        remaining -= day_count

    return sorted(schedule)


def ensure_file(filepath):
    dirpath = os.path.dirname(filepath)
    if dirpath:
        os.makedirs(dirpath, exist_ok=True)
    if not os.path.exists(filepath):
        with open(filepath, "w") as f:
            f.write(f"package main\n\n// {os.path.basename(filepath)}\n")


def make_commit(date, repo_path):
    rel_file = random.choice(GO_FILES)
    filepath = os.path.join(repo_path, rel_file)
    ensure_file(filepath)

    snippet = random.choice(GO_SNIPPETS)
    with open(filepath, "a") as f:
        f.write(snippet)

    message = random.choice(COMMIT_MESSAGES)

    subprocess.run(["git", "add", rel_file], cwd=repo_path)

    env = os.environ.copy()
    date_str = date.strftime("%Y-%m-%dT%H:%M:%S")
    env["GIT_AUTHOR_DATE"] = date_str
    env["GIT_COMMITTER_DATE"] = date_str

    subprocess.run(["git", "commit", "-m", message], cwd=repo_path, env=env)
    return rel_file, message


def main():
    print("=" * 60)
    print("golang-engineering-lab commit generator")
    print("=" * 60)

    num_commits = get_positive_int("How many commits to generate", 50)
    years = get_year_selection()
    repo_path = get_repo_path("Path to your local git repository", ".")

    year_label = " & ".join(str(y) for y in years)
    print(f"\nGenerating {num_commits} commits across {year_label} in: {repo_path}\n")

    schedule = build_commit_schedule(num_commits, years)

    for i, commit_date in enumerate(schedule):
        rel_file, message = make_commit(commit_date, repo_path)
        day_name = commit_date.strftime("%a")
        print(f"[{i+1}/{num_commits}] {commit_date.strftime('%Y-%m-%d')} ({day_name}) {commit_date.strftime('%H:%M')}  {message}  ({rel_file})")

    print("\nPushing to remote...")
    subprocess.run(["git", "push"], cwd=repo_path)
    print("Done. Check your GitHub contribution graph in a few minutes.")


if __name__ == "__main__":
    main()
