# Splatoon 3 read API (for the shared SplatNet site)

Plain HTTP, read-only, on the container's health listener (`HEALTH_LISTEN`, `:21013`), internal
network only: `http://openpak-splatoon-3:21013`. Not routed through Traefik. Nothing here is secret
and nothing is writable. Both answers come from the same loaded rotation and fest file the game is
served from, so the site and the console agree.

## `GET /api/v1/schedule.json[?from=RFC3339][&to=RFC3339]`

Default window: from two hours ago (the running slot) to 48 h ahead. `400` on a malformed time,
`503` while no rotation is loaded.

```json
{
  "game": "s3", "generated_at": "...", "set_id": "openpak-1790000000", "valid_until": "...",
  "vs": [{"start": "...", "end": "...", "modes": [
      {"kind": "regular",        "rule": 0, "rule_name": "turf_war",    "stages": [3, 9]},
      {"kind": "bankara_series", "rule": 2, "rule_name": "tower_control","stages": [..]},
      {"kind": "bankara_open",   "rule": 4, "rule_name": "clam_blitz",  "stages": [..]},
      {"kind": "x",              "rule": 1, "rule_name": "splat_zones", "stages": [..]},
      {"kind": "league",         "rule": 3, "rule_name": "rainmaker",   "stages": [..]}]}],
  "coop":   [{"start","end","shift_id","stage","boss","kuma_weapon","main_weapons"}],
  "season": [{"start","end"}]
}
```

- `vs` is 2-hour slots. Rule ids are the public SplatNet 3 ordering: 0 turf war, 1 splat zones,
  2 tower control, 3 rainmaker, 4 clam blitz. Ranked rules per slot come from `RankedRules`, the
  same function the gRPC service uses.
- `stages` are the game's numeric stage ids; the site owns the id -> name/image table.
- `coop.main_weapons` -1 = random, `kuma_weapon` -1 = random Grizzco weapon.
- The rotation is OpenPak-generated (not Nintendo's real rotation), renewed in memory hourly and
  rewritten to disk when it runs within 7 days of its end. `valid_until` is the end of the last slot.
- Normalised mapping for the site: `Rotation.slots[i] = {start, end, modes[]}`; `kind` maps to
  `regular | ranked(series/open) | x | league`; `coop` is a separate list.

## `GET /api/v1/fests.json`

```json
{"game": "s3", "fests": [{"id": "JUEA-00201", "state": "live", "regions": ["us"],
  "game_data": "juea-ability", "game_data_revision": "...", "teams": ["alpha","bravo","charlie"],
  "times": {"open","start","mid","end","close"}, "entries_per_team": {"alpha": 12}}]}
```

`state`: `scheduled` (before open, not served to consoles) | `announced` (open..start) | `live`
(start..end) | `final` (end..close) | `closed`. Empty `fests` when none is defined.
Results / winners are not available (see splatfest.md).
