---
name: create-pull-request
description: Commits changes, pushes to remote, and creates a GitHub Pull Request using the gh CLI. Use when the user asks to create a PR, push and open a pull request, or finalize and submit their implementation for review.
disable-model-invocation: true
---

# PR作成ワークフロー (Create Pull Request Workflow)

実装したコードのコミットから、リモートへのプッシュ、GitHub CLI (`gh`) を用いた PR 作成手順を定める。

## 1. 変更の確認とコミット

1. **未コミットの変更を確認**:
   ```bash
   git status
   git diff
   ```

2. **コミットの小口化**: 複数ファイルがある場合は `git add <対象ファイル>` と `git commit` を論理単位に分割する。

3. **コミットの実行**: メッセージ内の特殊文字（クォート、バッククォート、`\`、`$`）には細心の注意を払う。複雑なメッセージは `tmp/commit_msg.txt` に書き出して `git commit -F tmp/commit_msg.txt` を使用する。
   ```bash
   git commit -m "feat: add user authentication flow"
   ```

4. **コミットメッセージの確認**:
   ```bash
   git log -1
   ```
   問題があれば `git commit --amend` で修正する。

5. **プッシュ**:
   ```bash
   git push -u origin <ブランチ名>
   ```

## 2. Pull Request の作成

`GITHUB_TOKEN` を一時的に無効化して `gh` を実行する。タイトルと本文は**英語**で記述する。

```bash
GITHUB_TOKEN= gh pr create --title "Feature: ..." --body "## Description
...

## Changes
- ...

## Related Issues
closes #N"
```

作成後、出力された PR の URL をユーザーに報告する。

## 3. 追加改修の運用

### A. PR がまだオープンの場合
現在のブランチのまま修正 → `git add` → `git commit` → `git push` するだけで PR に反映される。

### B. PR マージ済みで追加改修が必要な場合
```bash
git checkout main && git pull origin main
git checkout -b <新しいブランチ名>
# 修正後、本ワークフローの手順 1〜2 を再実施
```
