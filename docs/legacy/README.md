# Historical TypeScript deployment

These files describe the working TypeScript installation at checkpoint `checkpoint-typescript-baseline`. They are retained for rollback and state migration; their commands require that checkout. The live configuration, database, media and Docker volumes have not been changed.

Recover a separate legacy checkout without modifying this Go branch:

```sh
git worktree add --detach ../bifrost-typescript checkpoint-typescript-baseline
```

The private environment files and runtime volumes are deliberately absent from Git. Follow the historical MOVING and RECOVERY instructions before any identity import. Do not start a second bridge against the live identity while the current bridge is running.

The complete original source, tests, Dockerfiles and package lock remain in the checkpoint. Source evidence links in OpenSpec point to that commit; they become browsable on GitHub when the commit is pushed. Before then use `git show checkpoint-typescript-baseline:<path>` locally.
