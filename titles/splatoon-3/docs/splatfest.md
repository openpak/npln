# Splatoon 3 Splatfest (`toyohr.v1.FestService`)

Status 2026-11-01. No console has seen any of this yet; everything below is built from the proto
and the notes in provenance.md. Test on Harbor first, one RPC at a time.

## Defining a fest

`fests.json` on the state volume (`NPLN_FEST_FILE`; default next to `rotation.json`, so
`/app/state/fests.json`). Re-read when the file changes, no restart. Format is documented at the top
of `npln/festfile.go`. A definition with times not strictly ordered `open < start < mid < end <
close` is skipped with a log line. Example replaying the captured pack in the prod BCAT dataset:

```json
{"fests":[{"id":"JUEA-00201","regions":[],"game_data":"juea-ability",
  "game_data_revision":"204e19c6...(from summary.yml)","teams":["alpha","bravo","charlie"],
  "open":"2026-11-07T00:00:00Z","start":"2026-11-08T00:00:00Z","mid":"2026-11-08T12:00:00Z",
  "end":"2026-11-09T00:00:00Z","close":"2026-11-09T06:00:00Z"}]}
```

## What is served

| RPC | Behaviour |
| --- | --- |
| `SelectFestSchedule` | the earliest-ending fest that is past `open`, not past `close`, in the requested region; else an empty response (what the game reads between fests). Name `tenants/t/festSchedules/{id}`, fest `tenants/t/fests/{id}`, `fest_game_data(_revision)` from the file, timetable, `fest_publishments` per region (open..close). |
| `CreateFestEntry` | stores the caller's team once per fest (first choice wins, a repeat returns it) in `festentries.json`; unknown team = INVALID_ARGUMENT; no fest / closed = FAILED_PRECONDITION. Entry name `tenants/t/fests/{f}/entries/{callerUserId}`. |
| `GetFestEntry` | the caller's entry for the fest in the name, else NOT_FOUND (headers frame first, Nintendo-shaped). The entry id the client sends is ignored. |
| `GetFestResult` | NOT_FOUND. **Gap**: results are server-computed from match results (the Pia referee path, an open gap); no tally exists. |
| `GetFestDecryptionKey` | NOT_FOUND. **Gap**: the pack is `.pack.zs.enc`; whether the key is per fest or per (fest, team) and whether a server-chosen key can decrypt a pack we re-encrypt is unknown without a console. We hold no key for the captured pack either. |

Consequence: the lobby can announce a fest and take a team choice; the fest screens that need the
decryption key will not complete until the key question is answered.

## Console tests still needed

1. Does the lobby accept a non-empty `SelectFestSchedule` (no 2162-0001 abort)? Which field formats
   does it parse (`target`, `fest_regions` casing, `fest_publishments` map key)?
2. What does `CreateFestEntry` send as `fest_team` (bare `alpha` or a resource name) and `parent`?
   The server stores whatever arrives; `hasTeam` compares the last path segment.
3. What `GetFestEntry` name does the client send, and does NOT_FOUND before choosing proceed to
   the team-pick screen?
4. Whether a missing key blocks the lobby or only the fest screen.
5. Region strings in `SelectFestScheduleRequest.region`.
