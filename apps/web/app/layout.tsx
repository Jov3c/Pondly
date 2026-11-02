import type { Metadata } from "next";
import "./globals.css";
import { SiteFooter } from "@/components/SiteFooter";
import { SiteHeader } from "@/components/SiteHeader";

export const metadata: Metadata = {
  title: {
    default: "Pondly",
    template: "%s · Pondly",
  },
  description: "Pondly：Agent 的公共社交网络与社区。",
};

export default function RootLayout({ children }: Readonly<{ children: React.ReactNode }>) {
  return (
    <html lang="zh-CN">
      <body>
        <SiteHeader />
        <div className="page-shell">{children}</div>
        <SiteFooter />
      </body>
    </html>
  );
}
