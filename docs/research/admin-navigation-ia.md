# Admin navigation information architecture

## Evidence

- [GitLab Admin area](https://docs.gitlab.com/administration/admin_area/) separates instance work into Overview, Users/Groups, and Monitoring. Its monitoring area includes system information, background jobs, logs, audit events, and statistics.
- [GitLab administration overview](https://docs.gitlab.com/administration/) uses task-oriented sections: Configure, Maintain, Monitor, Secure, and Administer users. This keeps settings, operations, security, and identity distinct.
- [GitLab security and compliance settings](https://docs.gitlab.com/administration/settings/security_and_compliance/) places security controls under Settings > Security and compliance rather than mixing them with runtime traffic views.
- [Harbor project navigation](https://goharbor.io/docs/edge/working-with-projects/create-projects/) keeps project-scoped destinations together as tabs: repositories, members, policy, robot accounts, logs, and configuration.
- [Gitea API administration](https://docs.gitea.com/api/) separates instance administration (users, organizations, runners, cron, system hooks) from repository operations and package registries.

## Findings for Depsilo

The old groups mixed four different user questions:

1. **Is the proxy healthy and what happened?** Dashboard, access logs, upstream updates, and bandwidth.
2. **How does traffic get served?** Upstreams, cache, index cache, and compiler cache.
3. **Can a request be trusted?** Security, quarantine, package rules, and audit logs.
4. **Who and how is the instance configured?** Projects, users, settings, and license.

“History”, “Sources & Cache”, “Security Governance”, and “System” describe implementation areas or time, but do not tell an operator which job the group supports. The new groups use the task language seen in GitLab and Gitea while retaining Harbor’s rule that related detail pages belong in local tabs.

## Final grouping (user-approved)

The final design uses the six resource areas requested by the user. Earlier
Monitor/Delivery groupings were intermediate proposals, not an industry standard.
The route manifest and DESIGN.md are the current implementation authorities.

| Group | Landing page | Local page navigation |
| --- | --- | --- |
| Overview | Dashboard | Dashboard, Bandwidth Report |
| Upstream Sources | Upstreams | None (one page) |
| Cache | Artifacts | Artifacts, Index Cache, Compiler Cache |
| Logs | Access Logs | Access Logs, Metadata Refreshes, Audit Logs |
| Security | Vulnerability Intelligence | Vulnerability Intelligence, Quarantine & Blocking, Package Rules |
| Projects | Projects | None (project list opens project details) |

The metadata refresh page describes cached-metadata revalidation, not changes to
upstream configuration. Audit Logs include management changes and package access,
so they belong with request and refresh records. Bandwidth is a usage/savings
report under Overview, rather than a cache-management action.

Instance-wide Users, Settings, and License are outside the six main destinations.
They share local navigation reached from **Instance management** at the bottom
of the sidebar, above the user information. The mobile drawer uses the same footer.

Inside the two Security pages, labeled native view selectors replace nested tab
bars. Intelligence retains Overview, Vulnerabilities, Suggested Rules, and Policies;
Quarantine retains Events, Approvals, and Malware blocklist. Existing functionality,
query activation, and intelligence deep links are preserved. Single-page groups
show no artificial tab bar, and no subpage labels are repeated in the sidebar.
