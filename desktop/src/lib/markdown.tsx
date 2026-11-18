// Markdown gọn cho câu trả lời của Trợ lý AI: đoạn văn, tiêu đề, danh sách, trích dẫn, khối code,
// **đậm**, *nghiêng*, `code`, [link](https://…) và link trần. Dựng thẳng phần tử React
// (không dùng HTML thô) nên nội dung từ AI không chèn được mã vào trang.
import { Fragment, type ReactNode } from "react";

const SAFE_URL = /^https?:\/\//i;

/** Định dạng trong một dòng. */
function inline(text: string, key: string): ReactNode[] {
  const out: ReactNode[] = [];
  // thứ tự: code, link, đậm, nghiêng, link trần
  const re =
    /(`[^`\n]+`)|\[([^\]\n]+)\]\((https?:\/\/[^\s)]+)\)|(\*\*|__)(.+?)\4|(?<![\p{L}\p{N}*_])([*_])(?!\s)(.+?)(?<!\s)\6(?![\p{L}\p{N}*_])|(https?:\/\/[^\s<>()]+[^\s<>().,;:!?'"])/gu;
  let last = 0;
  let i = 0;
  for (const m of text.matchAll(re)) {
    if (m.index! > last) out.push(text.slice(last, m.index));
    const k = `${key}-${i++}`;
    if (m[1]) out.push(<code key={k}>{m[1].slice(1, -1)}</code>);
    else if (m[2] && m[3] && SAFE_URL.test(m[3]))
      out.push(
        <a key={k} href={m[3]} target="_blank" rel="noreferrer noopener">
          {m[2]}
        </a>,
      );
    else if (m[4]) out.push(<strong key={k}>{inline(m[5], k)}</strong>);
    else if (m[6]) out.push(<em key={k}>{inline(m[7], k)}</em>);
    else if (m[8])
      out.push(
        <a key={k} href={m[8]} target="_blank" rel="noreferrer noopener">
          {m[8]}
        </a>,
      );
    last = m.index! + m[0].length;
  }
  if (last < text.length) out.push(text.slice(last));
  return out;
}

/** Ô bảng: chữ có định dạng, "<br>" thành xuống dòng. */
function cell(text: string, key: string): ReactNode[] {
  return lines(text.split(/<br\s*\/?>/i), key);
}

const TABLE_SEP = /^\s*\|?\s*:?-{2,}:?\s*(\|\s*:?-{2,}:?\s*)*\|?\s*$/;
const isTableStart = (src: string[], i: number) =>
  src[i].includes("|") && i + 1 < src.length && TABLE_SEP.test(src[i + 1]) && src[i + 1].includes("-");

function splitRow(line: string): string[] {
  let l = line.trim();
  if (l.startsWith("|")) l = l.slice(1);
  if (l.endsWith("|")) l = l.slice(0, -1);
  return l.split("|").map((c) => c.trim());
}

/** Ghép các dòng của một đoạn, xuống dòng giữ nguyên. */
function lines(list: string[], key: string): ReactNode[] {
  return list.map((l, i) => (
    <Fragment key={`${key}-${i}`}>
      {i > 0 && <br />}
      {inline(l, `${key}-${i}`)}
    </Fragment>
  ));
}

export function Markdown({ text }: { text: string }) {
  const src = text.replace(/\r\n?/g, "\n").split("\n");
  const blocks: ReactNode[] = [];
  let i = 0;
  let n = 0;
  const key = () => `b${n++}`;

  while (i < src.length) {
    const line = src[i];
    // khối code ```lang
    const fence = line.match(/^\s*```\s*([\w+-]*)\s*$/);
    if (fence) {
      const body: string[] = [];
      i++;
      while (i < src.length && !/^\s*```\s*$/.test(src[i])) body.push(src[i++]);
      i++; // bỏ dòng ``` đóng (nếu đang stream dở thì chưa có cũng không sao)
      blocks.push(
        <pre key={key()} className="md-code" data-lang={fence[1] || undefined}>
          <code>{body.join("\n")}</code>
        </pre>,
      );
      continue;
    }
    if (!line.trim()) {
      i++;
      continue;
    }
    // bảng: | a | b |  rồi dòng |---|---|
    if (isTableStart(src, i)) {
      const head = splitRow(src[i]);
      i += 2;
      const rows: string[][] = [];
      while (i < src.length && src[i].includes("|") && src[i].trim()) rows.push(splitRow(src[i++]));
      const k = key();
      blocks.push(
        <div key={k} className="md-table-wrap">
          <table className="md-table">
            <thead>
              <tr>
                {head.map((c, j) => (
                  <th key={j}>{cell(c, `${k}-h${j}`)}</th>
                ))}
              </tr>
            </thead>
            <tbody>
              {rows.map((r, ri) => (
                <tr key={ri}>
                  {head.map((_, j) => (
                    <td key={j}>{cell(r[j] ?? "", `${k}-${ri}-${j}`)}</td>
                  ))}
                </tr>
              ))}
            </tbody>
          </table>
        </div>,
      );
      continue;
    }
    const h = line.match(/^(#{1,4})\s+(.*)$/);
    if (h) {
      const k = key();
      blocks.push(
        <p key={k} className={`md-h md-h${h[1].length}`}>
          {inline(h[2], k)}
        </p>,
      );
      i++;
      continue;
    }
    if (/^\s*([-*_])\s*\1\s*\1[\s\-*_]*$/.test(line)) {
      blocks.push(<hr key={key()} className="md-hr" />);
      i++;
      continue;
    }
    if (/^\s*>/.test(line)) {
      const body: string[] = [];
      while (i < src.length && /^\s*>/.test(src[i])) body.push(src[i++].replace(/^\s*>\s?/, ""));
      // bên trong trích dẫn vẫn có thể có danh sách, khối code…
      blocks.push(
        <blockquote key={key()}>
          <Markdown text={body.join("\n")} />
        </blockquote>,
      );
      continue;
    }
    const bullet = /^(\s*)[-*+•]\s+(.*)$/;
    const ordered = /^(\s*)(\d+)[.)]\s+(.*)$/;
    if (bullet.test(line) || ordered.test(line)) {
      const isOl = ordered.test(line) && !bullet.test(line);
      const items: { text: string[]; sub: boolean }[] = [];
      let start = 1;
      while (i < src.length) {
        const l = src[i];
        const b = l.match(bullet);
        const o = l.match(ordered);
        if (b || o) {
          const indent = (b ? b[1] : o![1]).length;
          if (items.length === 0 && o) start = Number(o[2]);
          items.push({ text: [b ? b[2] : o![3]], sub: indent >= 2 });
          i++;
        } else if (l.trim() && /^\s{2,}/.test(l) && items.length) {
          items[items.length - 1].text.push(l.trim()); // dòng tiếp của mục
          i++;
        } else break;
      }
      const k = key();
      const lis = items.map((it, j) => (
        <li key={`${k}-${j}`} className={it.sub ? "md-sub" : undefined}>
          {lines(it.text, `${k}-${j}`)}
        </li>
      ));
      blocks.push(
        isOl ? (
          <ol key={k} start={start}>
            {lis}
          </ol>
        ) : (
          <ul key={k}>{lis}</ul>
        ),
      );
      continue;
    }
    // đoạn văn: gom tới dòng trống / khối khác
    const para: string[] = [];
    while (
      i < src.length &&
      src[i].trim() &&
      !/^\s*```/.test(src[i]) &&
      !/^#{1,4}\s/.test(src[i]) &&
      !/^\s*>/.test(src[i]) &&
      !bullet.test(src[i]) &&
      !ordered.test(src[i]) &&
      !isTableStart(src, i)
    ) {
      para.push(src[i++]);
    }
    const k = key();
    // dòng ghi nguồn "_— gemini · model_" ở cuối câu trả lời
    if (para.length === 1 && /^_— .+_$/.test(para[0].trim())) {
      blocks.push(
        <p key={k} className="md-source">
          {para[0].trim().slice(3, -1)}
        </p>,
      );
    } else blocks.push(<p key={k}>{lines(para, k)}</p>);
  }
  return <div className="md">{blocks}</div>;
}
