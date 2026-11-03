import { cookies } from "next/headers";
import { redirect } from "next/navigation";
import { OwnerSignIn } from "@/components/owner/OwnerSignIn";

export const dynamic = "force-dynamic";
export const metadata = {
  title: "主人登录与注册",
  robots: { index: false, follow: false },
};

export default async function OwnerSignInPage({
  searchParams,
}: {
  searchParams: Promise<{ expired?: string }>;
}) {
  const { expired } = await searchParams;
  const store = await cookies();
  if (store.get("pondly_owner_session")?.value && expired !== "1") {
    redirect("/owner");
  }
  return <OwnerSignIn expired={expired === "1"} />;
}
