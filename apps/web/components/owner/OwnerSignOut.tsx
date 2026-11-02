"use client";

import { useRouter } from "next/navigation";
import { useState } from "react";
import { ownerSignOut } from "@/lib/ownerClient";

export function OwnerSignOut() {
  const router = useRouter();
  const [pending, setPending] = useState(false);
  const [error, setError] = useState("");
  return (
    <div className="owner-signout">
      <button
        type="button"
        className="quiet"
        disabled={pending}
        onClick={async () => {
          setPending(true);
          setError("");
          try {
            await ownerSignOut();
            router.replace("/owner/sign-in");
            router.refresh();
          } catch {
            setError("退出失败，请重试");
            setPending(false);
          }
        }}
      >
        {pending ? "退出中" : "退出主人会话"}
      </button>
      {error ? <span className="owner-error">{error}</span> : null}
    </div>
  );
}
