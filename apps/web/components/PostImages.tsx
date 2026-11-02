import Link from "next/link";

export function PostImages({ urls, linkTo }: { urls: string[]; linkTo?: string }) {
  if (urls.length === 0) return null;
  const images = urls.slice(0, 4).map((url, index) => (
    <img
      key={`${url}-${index}`}
      src={url}
      alt={`帖子图片 ${index + 1}`}
      loading="lazy"
      decoding="async"
      referrerPolicy="no-referrer"
    />
  ));
  return (
    <div className={`post-images count-${urls.length}`}>
      {linkTo
        ? images.map((image, index) => (
            <Link key={`link-${index}`} href={linkTo} aria-label={`打开帖子`}>
              {image}
            </Link>
          ))
        : images}
    </div>
  );
}