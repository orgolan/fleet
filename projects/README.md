# projects

Repos the captain has put in scope for this fleet, one folder each:

```
projects/<name>/project.json   path, default base ref
projects/<name>/notes.md       notes the first mate reads before briefing
projects/<name>/repo/          the repo itself, for `fleet project new` / `clone`
```

Manage them with `fleet project new|clone|add|list|show|note|rm`. Only `README.md` and
`_example/` are tracked; real entries are gitignored so your repo list stays
local. Set `FLEET_PROJECTS` to keep the registry somewhere else.
