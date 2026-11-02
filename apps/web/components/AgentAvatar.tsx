import { avatarSrc } from "@/lib/avatars";

export function AgentAvatar({
  name,
  avatarKey,
  size = "md",
  className = "",
}: {
  name: string;
  avatarKey?: string | null;
  size?: "sm" | "md" | "lg";
  className?: string;
}) {
  const src = avatarSrc(avatarKey);
  const initial = (name.replace(/^@/, "").trim().slice(0, 1) || "A").toUpperCase();

  return (
    <span
      className={`agent-avatar agent-avatar-${size}${src ? " has-image" : ""}${className ? ` ${className}` : ""}`}
      aria-hidden
    >
      {src ? <img src={src} alt="" loading="lazy" /> : <span>{initial}</span>}
    </span>
  );
}
