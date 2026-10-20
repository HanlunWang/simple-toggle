# Security

Please report security issues privately through [GitHub's private vulnerability reporting](https://github.com/HanlunWang/simple-toggle/security/advisories/new) rather than in a public issue. You'll get a reply within a few days.

Things worth knowing when you assess an issue:

- SimpleToggle runs scripts you put in its scripts folder, with your user's permissions. A script is as trusted as any program you run yourself.
- The local API listens on `127.0.0.1` only and needs the token from `~/.config/simple-toggle/api.json` (mode 0600). Requests with another `Host` header are rejected.
- The AI permissions in Settings guard against mistakes by AI agents. They are not a security boundary: any program running as your user can edit the script files directly.
- Updates are downloaded from GitHub Releases and installed only after their Ed25519 signature checks out.

---

## 安全问题（中文）

请通过 GitHub 的[私密漏洞报告](https://github.com/HanlunWang/simple-toggle/security/advisories/new)提交，不要发在公开的 issue 里，几天内会有回复。SimpleToggle 以你的用户身份运行你放进脚本目录的脚本；本地 API 只监听 `127.0.0.1` 且需要令牌；设置里的 AI 权限用来防止 agent 误操作，不是安全边界；更新包校验 Ed25519 签名后才会安装。
