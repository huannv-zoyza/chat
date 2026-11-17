/** Chuẩn bị ảnh đại diện trước khi tải lên: cắt vuông ở giữa và thu nhỏ còn AVATAR_PX. */

export const AVATAR_PX = 256;
export const AVATAR_MAX_BYTES = 5 << 20;
export const AVATAR_ACCEPT = "image/png,image/jpeg,image/gif,image/webp";

function loadImage(file: Blob): Promise<HTMLImageElement> {
  return new Promise((resolve, reject) => {
    const url = URL.createObjectURL(file);
    const img = new Image();
    img.onload = () => {
      URL.revokeObjectURL(url);
      resolve(img);
    };
    img.onerror = () => {
      URL.revokeObjectURL(url);
      reject(new Error("Không đọc được ảnh này."));
    };
    img.src = url;
  });
}

/**
 * Ảnh GIF giữ nguyên (để còn chuyển động). Ảnh khác: cắt vuông, thu nhỏ, xuất WebP
 * (trình duyệt không hỗ trợ WebP thì tự xuất PNG). Trả về file sẵn sàng gửi lên server.
 */
export async function prepareAvatar(file: File): Promise<File> {
  if (!AVATAR_ACCEPT.split(",").includes(file.type)) {
    throw new Error("Chỉ nhận ảnh PNG, JPEG, GIF hoặc WebP.");
  }
  if (file.type === "image/gif") {
    if (file.size > AVATAR_MAX_BYTES) throw new Error("Ảnh GIF tối đa 5 MB.");
    return file;
  }

  const img = await loadImage(file);
  const side = Math.min(img.naturalWidth, img.naturalHeight);
  if (!side) throw new Error("Không đọc được ảnh này.");
  const out = Math.min(AVATAR_PX, side);
  const canvas = document.createElement("canvas");
  canvas.width = out;
  canvas.height = out;
  const ctx = canvas.getContext("2d");
  if (!ctx) throw new Error("Trình duyệt không xử lý được ảnh.");
  ctx.imageSmoothingQuality = "high";
  ctx.drawImage(
    img,
    (img.naturalWidth - side) / 2,
    (img.naturalHeight - side) / 2,
    side,
    side,
    0,
    0,
    out,
    out,
  );

  const blob = await new Promise<Blob | null>((resolve) => canvas.toBlob(resolve, "image/webp", 0.9));
  if (!blob) throw new Error("Không xử lý được ảnh này.");
  const ext = blob.type === "image/webp" ? "webp" : "png";
  return new File([blob], `avatar.${ext}`, { type: blob.type });
}
