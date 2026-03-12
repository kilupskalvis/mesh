---
tracker:
  kind: github
  owner: kilupskalvis
  repo: mesh
  label: mesh
  app_id: $GITHUB_APP_ID
  installation_id: $GITHUB_INSTALLATION_ID
  private_key_path: ~/.config/mesh/github-app.pem
polling:
  interval_ms: 30000
workspace:
  repo_url: https://github.com/kilupskalvis/mesh.git
server:
  port: 8080
---
You are working on issue {{ .Issue.Identifier }}.

{{ .Issue.Description }}
