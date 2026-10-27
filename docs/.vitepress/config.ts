import { defineConfig } from "vitepress";

export default defineConfig({
  lang: "en",
  title: "Gotham",
  description: "Self-hosted PaaS: control plane, node agent and web console.",
  cleanUrls: true,
  lastUpdated: true,
  themeConfig: {
    search: { provider: "local" },
    socialLinks: [{ icon: "github", link: "https://github.com/justindeelux/gotham" }],
  },
  locales: {
    root: {
      label: "English",
      lang: "en",
      themeConfig: {
        nav: [
          { text: "Guide", link: "/guide/" },
          { text: "Using Gotham", link: "/using/" },
          { text: "Operating Gotham", link: "/operating/" },
          { text: "Reference", link: "/reference/" },
        ],
        sidebar: {
          "/guide/": [
            {
              text: "Guide",
              items: [
                { text: "Getting started", link: "/guide/" },
                { text: "Install", link: "/guide/install" },
                { text: "First login", link: "/guide/first-login" },
                { text: "Domain and HTTPS", link: "/guide/domain-and-https" },
                { text: "Add nodes", link: "/guide/nodes" },
                { text: "Remote file service", link: "/guide/file-service" },
                { text: "Git sources and SSRF", link: "/guide/git-sources" },
                { text: "Updates", link: "/guide/updates" },
                { text: "Database versions and upgrades", link: "/guide/upgrade-databases" },
                { text: "Troubleshooting", link: "/guide/troubleshooting" },
              ],
            },
            {
              text: "Maintainers",
              items: [{ text: "Release and signing", link: "/guide/release-signing" }],
            },
          ],
          "/using/": [
            {
              text: "Using Gotham",
              items: [
                { text: "Overview", link: "/using/" },
                { text: "Projects and environments", link: "/using/projects-environments" },
                { text: "Applications", link: "/using/applications" },
                { text: "Databases", link: "/using/databases" },
                { text: "Services and templates", link: "/using/services-templates" },
                { text: "Domains and previews", link: "/using/domains-previews" },
                { text: "Members and access", link: "/using/members-access" },
                { text: "MCP setup", link: "/using/mcp" },
                { text: "Notifications", link: "/using/notifications" },
              ],
            },
          ],
          "/operating/": [
            {
              text: "Operating Gotham",
              items: [
                { text: "Overview", link: "/operating/" },
                { text: "Nodes and containers", link: "/operating/nodes-containers" },
                { text: "Host pressure monitoring", link: "/operating/monitoring" },
                { text: "Terminal and files", link: "/operating/terminal-files" },
                { text: "App store", link: "/operating/app-store" },
                { text: "Instance network", link: "/operating/instance-network" },
                { text: "Proxy and DNS", link: "/operating/proxy-dns" },
                { text: "Security", link: "/operating/security" },
              ],
            },
          ],
          "/reference/": [
            {
              text: "Reference",
              items: [
                { text: "Overview", link: "/reference/" },
                { text: "CLI", link: "/reference/cli" },
                { text: "Configuration", link: "/reference/configuration" },
                { text: "API tokens", link: "/reference/api-tokens" },
              ],
            },
          ],
        },
      },
    },
    vi: {
      label: "Tiếng Việt",
      lang: "vi",
      link: "/vi/",
      themeConfig: {
        nav: [
          { text: "Hướng dẫn", link: "/vi/guide/" },
          { text: "Sử dụng", link: "/vi/using/" },
          { text: "Vận hành", link: "/vi/operating/" },
          { text: "Tham chiếu", link: "/vi/reference/" },
        ],
        sidebar: {
          "/vi/guide/": [
            {
              text: "Hướng dẫn",
              items: [
                { text: "Bắt đầu", link: "/vi/guide/" },
                { text: "Cài đặt", link: "/vi/guide/install" },
                { text: "Đăng nhập lần đầu", link: "/vi/guide/first-login" },
                { text: "Tên miền và HTTPS", link: "/vi/guide/domain-and-https" },
                { text: "Thêm node", link: "/vi/guide/nodes" },
                { text: "Dịch vụ tệp từ xa", link: "/vi/guide/file-service" },
                { text: "Nguồn Git và SSRF", link: "/vi/guide/git-sources" },
                { text: "Cập nhật", link: "/vi/guide/updates" },
                { text: "Phiên bản cơ sở dữ liệu và nâng cấp", link: "/vi/guide/upgrade-databases" },
                { text: "Xử lý sự cố", link: "/vi/guide/troubleshooting" },
              ],
            },
            {
              text: "Người bảo trì",
              items: [{ text: "Phát hành và ký", link: "/vi/guide/release-signing" }],
            },
          ],
          "/vi/using/": [
            {
              text: "Sử dụng Gotham",
              items: [
                { text: "Tổng quan", link: "/vi/using/" },
                { text: "Dự án và môi trường", link: "/vi/using/projects-environments" },
                { text: "Ứng dụng", link: "/vi/using/applications" },
                { text: "Cơ sở dữ liệu", link: "/vi/using/databases" },
                { text: "Dịch vụ và template", link: "/vi/using/services-templates" },
                { text: "Tên miền và bản xem trước", link: "/vi/using/domains-previews" },
                { text: "Thành viên và quyền truy cập", link: "/vi/using/members-access" },
                { text: "Thiết lập MCP", link: "/vi/using/mcp" },
                { text: "Thông báo", link: "/vi/using/notifications" },
              ],
            },
          ],
          "/vi/operating/": [
            {
              text: "Vận hành Gotham",
              items: [
                { text: "Tổng quan", link: "/vi/operating/" },
                { text: "Node và container", link: "/vi/operating/nodes-containers" },
                { text: "Giám sát áp lực máy chủ", link: "/vi/operating/monitoring" },
                { text: "Terminal và tệp", link: "/vi/operating/terminal-files" },
                { text: "Kho ứng dụng", link: "/vi/operating/app-store" },
                { text: "Mạng của instance", link: "/vi/operating/instance-network" },
                { text: "Proxy và DNS", link: "/vi/operating/proxy-dns" },
                { text: "Bảo mật", link: "/vi/operating/security" },
              ],
            },
          ],
          "/vi/reference/": [
            {
              text: "Tham chiếu",
              items: [
                { text: "Tổng quan", link: "/vi/reference/" },
                { text: "CLI", link: "/vi/reference/cli" },
                { text: "Cấu hình", link: "/vi/reference/configuration" },
                { text: "API token", link: "/vi/reference/api-tokens" },
              ],
            },
          ],
        },
      },
    },
  },
});
