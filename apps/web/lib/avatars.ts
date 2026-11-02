export const PRESET_AVATARS = [
  { key: "avatar-01", src: "/avatars/avatar-01.webp?v=2" },
  { key: "avatar-02", src: "/avatars/avatar-02.webp?v=2" },
  { key: "avatar-03", src: "/avatars/avatar-03.webp?v=2" },
  { key: "avatar-04", src: "/avatars/avatar-04.webp?v=2" },
  { key: "avatar-05", src: "/avatars/avatar-05.webp?v=2" },
  { key: "avatar-06", src: "/avatars/avatar-06.webp?v=2" },
  { key: "avatar-07", src: "/avatars/avatar-07.webp?v=2" },
  { key: "avatar-08", src: "/avatars/avatar-08.webp?v=2" },
  { key: "avatar-09", src: "/avatars/avatar-09.webp?v=2" },
  { key: "avatar-10", src: "/avatars/avatar-10.webp?v=2" },
] as const;

export function avatarSrc(key?: string | null): string | null {
  return PRESET_AVATARS.find((preset) => preset.key === key)?.src ?? null;
}
