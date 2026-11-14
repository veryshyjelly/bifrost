# Public repository checks

Keep deployment environments, credentials, paired sessions, media, databases and
backups outside Git. `.gitignore` excludes these files; it cannot remove anything
already committed. Use fictional identities and contact details in test fixtures.

The `Public history` workflow scans every fetched branch and tag with Gitleaks
8.30.1. To repeat the check locally after installing that release:

```sh
git fetch origin --prune --tags
gitleaks git . --config .gitleaks.toml --log-opts="--all --full-history" \
  --redact --ignore-gitleaks-allow
git log --all --format='%an <%ae> | %cn <%ce>' | sort -u
```

Review both author and committer identities, commit messages, filenames and
historical file contents. Scanner success alone does not establish that personal
information is absent. `.gitleaks.toml` allows only reviewed build pins, source
hashes and one nonfunctional example, scoped to their specific files and lines.
It also checks for common forms of personal operational narrative in Markdown.
Keep actual host devices, operating timelines, account inventories, host-transfer
results and personal performance records private. Setup guides should describe
supported configurations and verification steps without identifying anyone's
installation.

Historical source references use `checkpoint-*` tags so links survive the privacy
rewrite. Preserve these tags when pushing or cloning the documented checkpoints.

Before publishing rewritten history, coordinate with anyone who has a clone and
update each remote branch using its recorded old tip with `--force-with-lease`.
Avoid a mirror push, which can delete unrelated remote refs. Then audit a fresh
clone from the remote, including all branches and tags. Old clones, forks,
pull-request refs and GitHub cached commit pages can retain earlier objects;
history rewriting does not erase those copies. If actual credentials are ever
found, rotate them as well as removing them from history.

If old commit metadata must remain private, a fresh, unrelated repository avoids
publishing the original repository's cached objects. See GitHub's
[history-removal limitations](https://docs.github.com/en/authentication/keeping-your-account-and-data-secure/removing-sensitive-data-from-a-repository).

Keep recovery bundles and detailed scanner reports private and outside this
repository. Rewriting history and pushing it do not change GitHub visibility;
visibility is a separate owner action.
