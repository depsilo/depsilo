# Admin navigation information architecture

## Evidence

- [GitLab Admin area](https://docs.gitlab.com/administration/admin_area/) separates instance work into Overview, Users/Groups, and Monitoring. Its monitoring area includes system information, background jobs, logs, audit events, and statistics.
- [GitLab administration overview](https://docs.gitlab.com/administration/) uses task-oriented sections: Configure, Maintain, Monitor, Secure, and Administer users. This keeps settings, operations, security, and identity distinct.
- [GitLab security and compliance settings](https://docs.gitlab.com/administration/settings/security_and_compliance/) places security controls under Settings > Security and compliance rather than mixing them with runtime traffic views.
- [Harbor project navigation](https://goharbor.io/docs/edge/working-with-projects/create-projects/) keeps project-scoped destinations together as tabs: repositories, members, policy, robot accounts, logs, and configuration.
- [Gitea API administration](https://docs.gitea.com/api/) separates instance administration (users, organizations, runners, cron, system hooks) from repository operations and package registries.

## Findings for Depsilo

The old groups mixed three different user questions:

1. **Is the proxy healthy and what happened?** Dashboard, access logs, upstream updates, and bandwidth.
2. **How does traffic get served?** Upstreams, cache, index cache, and compiler cache.
3. **Can a request be trusted?** Security, quarantine, package rules, and audit logs.
4. **Who and how is the instance configured?** Projects, users, settings, and license.

“History”, “Sources & Cache”, “Security Governance”, and “System” describe implementation areas or time, but do not tell an operator which job the group supports. The new groups use the task language seen in GitLab and Gitea while retaining Harbor’s rule that related detail pages belong in local tabs.

## Adopted grouping

| Group | Landing page | Tabs | User question |
| --- | --- | --- | --- |
| Overview | Dashboard | none | What needs attention now? |
| Upstream Sources | Upstreams | Upstreams, Upstream Updates | Where do dependencies come from? |
| Cache | Cache | Cache, Index Cache, Compiler Cache, Bandwidth | How are dependencies stored and accelerated? |
| Logs | Access Logs | Access Logs | What requests reached the proxy? |
| Security | Security | Audit Logs, Security, Quarantine, Rules | Can requests and changes be trusted? |
| Projects | Projects | Projects | Which project workspaces exist? |

Instance-wide Users, Settings, and License are deliberately outside the six
task domains. The shell exposes them through the top-right **Instance
management** control and keeps their three-page local navigation together.

The sidebar keeps only these five task-level links. Page tabs expose the detailed destinations, so a route appears once in the persistent shell and once in the relevant local context rather than being repeated as both a sidebar leaf and a tab.
