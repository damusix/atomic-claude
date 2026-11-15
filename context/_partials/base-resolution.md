{{define "base-resolution"}}```
   BASE=$(git symbolic-ref --short refs/remotes/origin/HEAD 2>/dev/null) && BASE=${BASE#origin/}
   [ -n "$BASE" ] || for b in main master; do
     git show-ref --verify --quiet "refs/heads/$b" && BASE=$b && break
   done
   ```

   When `BASE` is still empty, run the `default-branch` operation from the source-control reference that the resolved `<workflow-preferences>` block lists under `Read:`. With no resolved block in context, run `atomic workflow resolve` once first.{{- end}}
