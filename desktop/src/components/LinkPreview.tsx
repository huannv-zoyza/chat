import { useEffect, useState, type MouseEvent } from "react";
import type { LinkPreview as Preview } from "../lib/api";
import { extractLinks } from "../lib/links";
import { openExternal } from "../lib/notify";
import { isTauri } from "../lib/platform";
import { useChat } from "../state/ChatProvider";
import { Icon } from "./ui";

/**
 * Mở link: trên app desktop nhờ trình duyệt mặc định (webview không tự mở tab mới),
 * trên web để trình duyệt mở tab mới như thường.
 */
export function onLinkClick(e: MouseEvent<HTMLAnchorElement>) {
  if (!isTauri) return;
  e.preventDefault();
  void openExternal(e.currentTarget.href);
}

export function ExternalLink({ href, children, className }: { href: string; children: React.ReactNode; className?: string }) {
  return (
    <a href={href} target="_blank" rel="noopener noreferrer" onClick={onLinkClick} className={className}>
      {children}
    </a>
  );
}

/** Thẻ xem trước cho các link trong tin (tối đa 3). */
export function LinkPreviews({ content }: { content: string }) {
  const links = extractLinks(content);
  if (links.length === 0) return null;
  return (
    <div className="link-previews">
      {links.map((u) => (
        <LinkCard key={u} url={u} />
      ))}
    </div>
  );
}

function LinkCard({ url }: { url: string }) {
  const { api } = useChat();
  const [p, setP] = useState<Preview | null>(null);
  const [playing, setPlaying] = useState(false);
  const [imgBroken, setImgBroken] = useState(false);

  useEffect(() => {
    let alive = true;
    api.unfurl(url).then((res) => alive && setP(res));
    return () => {
      alive = false;
    };
  }, [api, url]);

  if (!p || p.kind === "none") return null;

  if (p.kind === "video") {
    return (
      <video className="media-player media-video" src={url} controls preload="metadata" playsInline>
        <ExternalLink href={url}>Mở video</ExternalLink>
      </video>
    );
  }
  if (p.kind === "audio") {
    return <audio className="media-player media-audio" src={url} controls preload="metadata" />;
  }
  if (p.kind === "image") {
    return (
      <ExternalLink href={url} className="link-image">
        <img src={url} alt="" loading="lazy" />
      </ExternalLink>
    );
  }

  if (p.kind === "youtube") {
    return (
      <div className="link-card link-video">
        <div className="link-text">
          <span className="link-site">{p.site_name}</span>
          {p.title && (
            <ExternalLink href={url} className="link-title">
              {p.title}
            </ExternalLink>
          )}
          {p.description && <span className="link-desc">{p.description}</span>}
        </div>
        <div className="video-frame">
          {playing && p.embed_url ? (
            <iframe
              src={p.embed_url}
              title={p.title ?? "Video YouTube"}
              allow="autoplay; encrypted-media; picture-in-picture; fullscreen"
              allowFullScreen
            />
          ) : (
            <button type="button" className="video-poster" onClick={() => setPlaying(true)} aria-label={`Phát ${p.title ?? "video"}`}>
              {p.image && <img src={p.image} alt="" loading="lazy" />}
              <span className="play-btn" aria-hidden>
                <Icon name="play" size={26} />
              </span>
            </button>
          )}
        </div>
      </div>
    );
  }

  // Trang web thường: tên trang, tiêu đề, mô tả, ảnh nhỏ bên phải
  const showImg = p.image && !imgBroken;
  return (
    <div className={`link-card${showImg ? " has-thumb" : ""}`}>
      <div className="link-text">
        {p.site_name && <span className="link-site">{p.site_name}</span>}
        {p.title && (
          <ExternalLink href={url} className="link-title">
            {p.title}
          </ExternalLink>
        )}
        {p.description && <span className="link-desc">{p.description}</span>}
      </div>
      {showImg && (
        <ExternalLink href={url} className="link-thumb">
          <img src={p.image} alt="" loading="lazy" onError={() => setImgBroken(true)} referrerPolicy="no-referrer" />
        </ExternalLink>
      )}
    </div>
  );
}
