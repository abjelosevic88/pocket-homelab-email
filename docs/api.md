# API

JSON over HTTP under `/api/v1`. Authenticate with the session cookie, a personal token (`Authorization: Bearer pml_…`, Settings → API tokens), a Pocket Core token, or a Pocket Core link token (`pcx_…`, only the routes of its scope). Everything is per person: other people's ids answer 404.

## Practices

| | |
|---|---|
| `GET /practices?from&to&kind&technique&program&source&q&notes=1&limit&offset` | Newest first, `{practices, total}` |
| `POST /practices` | Log a practice (below); answers `{practice, unlocked, lesson_completed, program, day_minutes, goal_min, streak}` |
| `GET/PUT/DELETE /practices/{id}` | One practice; PUT takes any subset of the fields |
| `GET /practices/tags` | Tags with counts |

```json
{"kind": "breath", "technique": "box", "started_at": "2026-10-25T07:30:00+02:00", "duration_s": 300, "planned_s": 300,
 "completed": true, "cycles": 18, "calm_before": 2, "calm_after": 4, "note": "", "tags": "morning",
 "program": "path", "lesson": "settle-4", "external_id": "uuid-from-the-client"}
```

`kind` is `breath`, `meditation`, `body_scan`, `loving_kindness`, `walking` or `other` (guessed from `technique` when left out). `calm_*` is 1 (tense) to 5 (calm). The same `external_id` sent again updates instead of doubling. `program` + `lesson` finish that lesson when the practice is completed.

## Library

| | |
|---|---|
| `GET /library` | Built-in breathing patterns and meditation styles, kinds, bells, background sounds, your presets, favourites, how often each technique was used |
| `GET /techniques/{key}` | One pattern or style |
| `PUT /favorites/{key}` `{"on": true}` | Star a built-in technique |
| `GET/POST /presets`, `PUT/DELETE /presets/{id}` | Your own breathing patterns (`kind: breath`, `config: {phases: [{type: in\|in2\|hold\|out\|hold_out, s, label?}], minutes \| cycles, ease}`) and timers (`kind: timer`, `config: {minutes, warmup_s, interval_min, start_bell, interval_bell, end_bell, ambient, style, guidance}`) |

## The Path and courses

| | |
|---|---|
| `GET /programs` | Every program with your progress and next lesson |
| `GET /programs/{key}` | Stages and lessons (done, locked, current) |
| `POST /programs/{key}/start`, `POST /programs/{key}/reset` | |
| `POST /programs/{key}/lessons/{lesson}/complete` | Mark a lesson done without a timed practice (409 while it is locked) |

## Overview and statistics

| | |
|---|---|
| `GET /home` | Today: goal, streak, level, week, next lesson, suggestion, quick starts, recent practices, latest milestones |
| `GET /dashboard` | Small summary for Homepage widgets |
| `GET /stats?range=w\|m\|3m\|y\|all` | Totals, previous period, streaks, daily/weekly/monthly series, year heatmap, by technique (with calm before → after), by kind, by hour, weekdays, lengths, mood, goals, insights |
| `GET /stats/health?range=3m` | Heart rate variability, resting heart rate and sleep on days with practice vs without (needs Pocket Health) |
| `GET /achievements` | Milestones and levels |
| `GET /day/{date}` | One day for journals (Pocket Day) |
| `GET /overlay?from&to` | Practices as timed calendar items (Pocket Calendar) |

## Connections

| | |
|---|---|
| `GET/POST /rules`, `PUT/DELETE /rules/{id}` | Habit rules: `{kind: any\|<kind>, min_minutes, habit_id, habit_name, enabled}` |
| `GET /habits/list` | Your habits in Pocket Habits |
| `GET /music/playlists`, `GET /music/playlists/{id}`, `GET /music/stream/{songId}` | Navidrome playlists for background music (streamed through Still) |
| `POST /ai/reflect` `{range}`, `POST /ai/ask` `{question}` | Reflection on your practice; questions about it |

## Pocket Core

Offers `mindfulness:read` (dashboard, day, overlay, stats, practices without notes, programs, milestones) and `mindfulness:write` (log practices; change only its own). Uses Pocket Habits (`habits:read`, `entries:write`), Pocket Health (`health:write` for mindful minutes, `health:read` for the comparison), Pocket AI and the Navidrome service. Events: `still.practice_completed`, `still.lesson_completed`, `still.program_completed`, `still.milestone_reached`.

## Data

`GET /export.json`, `GET /export.csv`, `POST /import` (a Pocket Mail export; importing twice adds nothing), `GET /backup` (admin, the whole database).
