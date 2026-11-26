import {
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
  type ReactNode,
} from "react";
import { createPortal } from "react-dom";
import { ApiError, type Wallet } from "../lib/api";
import { displayName } from "../lib/format";
import {
  farmEvents,
  farmScreen,
  formatGrow,
  formatLeft,
  useFarmOpen,
  type Animal,
  type Crop,
  type FarmLog,
  type FarmView,
  type LeaderRow,
  type Neighbor,
  type Plot,
  type PlotState,
  type Quest,
  type Item,
  type Recipe,
  type Spot,
  spotProblem,
} from "../lib/farm";
import { playCoinSound } from "../lib/sound";
import { useChat } from "../state/ChatProvider";
import { Emo, emo } from "./Emoji";
import { FarmWorld } from "./FarmWorld";
import { Avatar, Dialog, EmptyState, Icon } from "./ui";

const errMsg = (err: unknown) =>
  err instanceof ApiError ? err.message : "Có lỗi xảy ra, thử lại sau.";

/* =========================================================
   Đồng hồ: lệch giữa máy chủ và máy mình, tích tắc mỗi giây
   ========================================================= */

function useServerNow(serverNow: string | undefined) {
  const offset = useRef(0);
  useEffect(() => {
    if (serverNow) offset.current = new Date(serverNow).getTime() - Date.now();
  }, [serverNow]);
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    const t = window.setInterval(() => setNow(Date.now()), 1000);
    return () => window.clearInterval(t);
  }, []);
  return now + offset.current;
}

/** Trạng thái ô tính lại theo đồng hồ (máy chủ gửi lúc tải, cây vẫn lớn tiếp giữa hai lần tải). */
function liveState(
  p: Plot,
  now: number,
): { state: PlotState; progress: number; left: number } {
  if (p.state === "empty") return { state: "empty", progress: 0, left: 0 };
  const planted = p.planted_at ? new Date(p.planted_at).getTime() : now;
  const ready = p.ready_at ? new Date(p.ready_at).getTime() : now;
  const wither = p.wither_at ? new Date(p.wither_at).getTime() : Infinity;
  if (p.state === "withered" || now >= wither)
    return { state: "withered", progress: 1, left: 0 };
  if (now >= ready) return { state: "ripe", progress: 1, left: wither - now };
  const total = Math.max(1, ready - planted);
  return {
    state: "growing",
    progress: Math.min(0.999, Math.max(0, (now - planted) / total)),
    left: ready - now,
  };
}

/** Ngày / chiều / tối theo giờ máy người xem: trời đổi màu theo. */
function timeOfDay(d: Date): "dawn" | "day" | "dusk" | "night" {
  const h = d.getHours() + d.getMinutes() / 60;
  if (h >= 5 && h < 6.5) return "dawn";
  if (h >= 6.5 && h < 17) return "day";
  if (h >= 17 && h < 18.75) return "dusk";
  return "night";
}

/* =========================================================
   Màn hình nông trại (toàn màn hình)
   ========================================================= */

export function FarmOverlay() {
  const open = useFarmOpen();
  useEffect(() => {
    if (!open) return;
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape" && !document.querySelector("dialog[open]"))
        farmScreen.close();
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [open]);
  if (!open) return null;
  return createPortal(<Farm />, document.body);
}

type Tab =
  | "quests"
  | "orders"
  | "storage"
  | "kitchen"
  | "shop"
  | "neighbors"
  | "log"
  | "top";
const TABS: { id: Tab; label: string; icon: string }[] = [
  { id: "quests", label: "Nhiệm vụ", icon: "📋" },
  { id: "orders", label: "Đơn hàng", icon: "🚚" },
  { id: "storage", label: "Kho", icon: "🏚️" },
  { id: "kitchen", label: "Xưởng", icon: "🍳" },
  { id: "shop", label: "Cửa hàng", icon: "🛒" },
  { id: "neighbors", label: "Hàng xóm", icon: "🏡" },
  { id: "log", label: "Nhật ký", icon: "📜" },
  { id: "top", label: "Xếp hạng", icon: "🏆" },
];

type Pop = {
  id: number;
  plot: number;
  text: string;
  tone: "xu" | "xp" | "bad" | "info" | "love";
};
let popSeq = 0;

function Farm() {
  const { api, state, dispatch } = useChat();
  const [mine, setMine] = useState<FarmView | null>(null);
  const [visit, setVisit] = useState<FarmView | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const [tab, setTab] = useState<Tab>("quests");
  const [seedFor, setSeedFor] = useState<number | null>(null);
  const [pops, setPops] = useState<Pop[]>([]);
  const [toast, setToast] = useState<{
    text: string;
    tone: "good" | "bad" | "level";
  } | null>(null);
  const [newLogs, setNewLogs] = useState(0);
  // mua / xây từ bản đồ phải hỏi lại: chạm nhầm trên bản đồ dễ hơn bấm nút trong cửa hàng
  const [confirm, setConfirm] = useState<ConfirmBuy | null>(null);
  const [giftOpen, setGiftOpen] = useState(false);
  // đang chọn chỗ xây / dời công trình tự do (xưởng...) trên bản đồ
  const [placing, setPlacing] = useState<{
    id: string;
    mode: "build" | "move";
    spot: Spot;
  } | null>(null);
  const view = visit ?? mine;
  const now = useServerNow(view?.now);
  // đang thăm vườn người khác: Esc là về nhà (Esc ở nhà mới đóng nông trại, xem FarmOverlay)
  useEffect(() => {
    if (!visit) return;
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape" && !document.querySelector("dialog[open]")) {
        e.stopImmediatePropagation();
        setVisit(null);
      }
    };
    window.addEventListener("keydown", onKey, true);
    return () => window.removeEventListener("keydown", onKey, true);
  }, [visit]);

  const showToast = useCallback(
    (text: string, tone: "good" | "bad" | "level" = "good") => {
      setToast({ text, tone });
      window.setTimeout(
        () => setToast((t) => (t?.text === text ? null : t)),
        tone === "level" ? 4000 : 3500,
      );
    },
    [],
  );
  const pop = useCallback(
    (plot: number, text: string, tone: Pop["tone"], delay = 0) => {
      const id = ++popSeq;
      window.setTimeout(() => {
        setPops((p) => [...p, { id, plot, text, tone }]);
        window.setTimeout(
          () => setPops((p) => p.filter((x) => x.id !== id)),
          1400,
        );
      }, delay);
    },
    [],
  );

  const loadMine = useCallback(
    () =>
      api
        .getFarm()
        .then((f) => {
          // chỉ xoá lỗi "không tải được" của lần mở đầu; lỗi của một hành động (vd. thiếu thức ăn)
          // phải giữ lại cho người ta đọc, không bị lần tải lại ngay sau đó xoá mất
          setMine((prev) => {
            if (!prev) setError(null);
            return f;
          });
        })
        .catch((err) => setError(errMsg(err))),
    [api],
  );

  // thông báo lỗi tự ẩn sau 5 giây (bấm vào để ẩn ngay)
  useEffect(() => {
    if (!error) return;
    const t = window.setTimeout(() => setError(null), 5000);
    return () => window.clearTimeout(t);
  }, [error]);

  useEffect(() => {
    void loadMine();
  }, [loadMine]);

  // làm mới định kỳ nông trại đang xem (người khác có thể vừa trộm / tưới)
  useEffect(() => {
    const t = window.setInterval(() => {
      if (visit)
        api
          .visitFarm(visit.user.id)
          .then(setVisit)
          .catch(() => {});
      else void loadMine();
    }, 20_000);
    return () => window.clearInterval(t);
  }, [api, visit, loadMine]);

  // có người vừa tác động lên nông trại của mình
  useEffect(
    () =>
      farmEvents.on((l) => {
        void loadMine();
        setNewLogs((n) => n + 1);
        showToast(
          logText(l, mine?.crops ?? [], mine?.items),
          l.action === "steal" || l.action === "breakup" ? "bad" : "good",
        );
      }),
    [loadMine, showToast, mine?.crops],
  );

  /** Gọi một hành động, áp kết quả, bay số "+xu" lên ô vừa làm. */
  const act = async (
    plot: number | null,
    fn: () => Promise<FarmView>,
    okText?: string,
  ) => {
    if (busy) return;
    setBusy(true);
    setError(null);
    try {
      const f = await fn();
      if (f.wallet)
        dispatch({
          type: "wallet.set",
          balance: f.wallet.balance,
          debt: f.wallet.debt,
        });
      if (f.me) {
        setMine(f);
        setVisit(null);
      } else {
        setVisit(f);
        // hành động ở nhà người khác cũng đổi ví / nhiệm vụ của mình
        void loadMine();
      }
      const g = f.gained;
      const at = plot ?? 0;
      if (f.dog) {
        pop(at, `🐕 −${Math.abs(g?.xu ?? 10)} xu`, "bad");
        showToast("Chó giữ nhà đuổi bạn chạy mất dép! 🐕", "bad");
      } else if (g) {
        // vật vào kho (thu hoạch, thu trứng sữa, trộm): "+3 🍅"
        const catalog = f.items ?? mine?.items ?? [];
        Object.entries(g.items ?? {}).forEach(([id, n], i) => {
          const e =
            catalog.find((x) => x.id === id)?.emoji ??
            f.crops.find((c) => c.id === id)?.emoji ??
            "📦";
          pop(at, `+${n} ${e}`, "xu", i * 160);
        });
        if (g.xu)
          pop(at, `${g.xu > 0 ? "+" : ""}${g.xu} 🪙`, g.xu > 0 ? "xu" : "bad");
        if (g.xp) pop(at, `+${g.xp} XP`, "xp", 180);
        if (g.love) pop(at, `+${g.love} 💗`, "love", 320);
        if (g.xu > 0) playCoinSound();
      }
      if (g?.love_up) {
        const c = f.couple ?? mine?.couple;
        showToast(
          `💞 Thân mật lên cấp ${g.love_up}${c ? `: ${c.level_name}` : ""}! Cả hai cùng nhận ${30 * g.love_up} xu.`,
          "level",
        );
      } else if (g?.level_up)
        showToast(
          `🎉 Lên cấp ${g.level_up}! Mở khoá cây mới trong cửa hàng hạt.`,
          "level",
        );
      else if (okText) showToast(okText);
      return true;
    } catch (err) {
      setError(errMsg(err));
      if (view && !view.me)
        api
          .visitFarm(view.user.id)
          .then(setVisit)
          .catch(() => {});
      else void loadMine();
    } finally {
      setBusy(false);
    }
    return false;
  };

  const goVisit = async (userId: number) => {
    setError(null);
    try {
      setVisit(await api.visitFarm(userId));
      setSeedFor(null);
      if (window.innerWidth <= 860)
        document
          .querySelector(".farm-scroll")
          ?.scrollTo({ top: 0, behavior: "smooth" });
    } catch (err) {
      setError(errMsg(err));
    }
  };

  const onPlot = (p: Plot) => {
    if (!view || busy) return;
    const live = liveState(p, now);
    const uid = view.user.id;
    if (view.me) {
      if (live.state === "empty") return setSeedFor(p.index);
      if (live.state === "ripe")
        return void act(p.index, () => api.farmHarvest(p.index));
      if (live.state === "withered")
        return void act(p.index, () => api.farmClear(p.index), "Đã dọn ô đất.");
      if (p.bug)
        return void act(
          p.index,
          () => api.farmBug(uid, p.index),
          "Đã bắt sâu 🐛",
        );
      if (p.can_water)
        return void act(
          p.index,
          () => api.farmWater(uid, p.index),
          "Đã tưới, cây lớn nhanh hơn 💧",
        );
      return showToast(`Cây đang lớn, còn ${formatLeft(live.left)} nữa.`);
    }
    if (live.state === "ripe" && p.can_steal)
      return void act(p.index, () => api.farmSteal(uid, p.index));
    if (p.bug && p.can_bug)
      return void act(
        p.index,
        () => api.farmBug(uid, p.index),
        "Bắt sâu giúp hàng xóm 🐛",
      );
    if (live.state === "growing" && p.can_water)
      return void act(
        p.index,
        () => api.farmWater(uid, p.index),
        "Tưới giúp hàng xóm 💧",
      );
    return showToast(plotHint(p, live.state, false, view));
  };

  const onAnimal = (a: Animal) => {
    if (!view?.me || busy) return;
    const k = view.animal_kinds?.find((x) => x.id === a.kind);
    const ready =
      a.state === "ready" ||
      (a.state === "producing" &&
        !!a.ready_at &&
        new Date(a.ready_at).getTime() <= now);
    if (ready) return void act(-1, () => api.farmCollect(a.id));
    if (
      a.state === "hungry" &&
      k &&
      (view.storage?.stock[k.feed] ?? 0) < k.feed_qty
    ) {
      // thiếu thức ăn: nói rõ thiếu gì và mở luôn chợ trong tab Kho
      const feed = view.items?.find((i) => i.id === k.feed);
      setTab("storage");
      return showToast(
        `${k.name} cần ${k.feed_qty} ${feed?.emoji ?? ""} ${feed?.name.toLowerCase() ?? k.feed}. Trồng thêm hoặc mua ở Chợ bên phải.`,
        "bad",
      );
    }
    if (a.state === "hungry")
      return void act(
        -1,
        () => api.farmFeed(a.id),
        `Đã cho ${k?.name.toLowerCase() ?? "con vật"} ăn 🍽️`,
      );
    const left = a.ready_at ? new Date(a.ready_at).getTime() - now : 0;
    showToast(
      `${k?.name ?? "Con vật"} đang làm ra sản phẩm, còn ${formatLeft(left)}.`,
    );
  };

  /** Điện thoại: bảng mục nằm dưới cánh đồng, bấm nhà kho / xe tải / xưởng thì cuộn xuống cho thấy. */
  const scrollToPanel = () => {
    if (window.innerWidth > 960) return;
    window.setTimeout(
      () =>
        document
          .querySelector(".farm-panel")
          ?.scrollIntoView({ behavior: "smooth", block: "start" }),
      30,
    );
  };
  const openTab = (t: Tab) => {
    setTab(t);
    scrollToPanel();
  };

  /** Bắt đầu chọn chỗ xây / dời công trình tự do id: hiện bản nháp trên bản đồ, kéo hoặc chạm để dời. */
  const startPlacing = (id: string, mode: "build" | "move") => {
    const b = mine?.buildings?.find((x) => x.id === id);
    if (!b?.spot) return;
    setVisit(null);
    setPlacing({ id, mode, spot: b.spot });
    // điện thoại: bản đồ ở trên bảng mục, cuộn lên cho thấy
    document.querySelector(".farm-scroll")?.scrollTo({ top: 0, behavior: "smooth" });
  };
  const placeB = placing
    ? mine?.buildings?.find((b) => b.id === placing.id)
    : undefined;
  const placeProblem =
    placing && mine ? spotProblem(mine, placing.id, placing.spot) : null;
  const confirmPlace = async () => {
    if (!placing || !placeB || placeProblem) return;
    const { id, mode, spot } = placing;
    const ok = await act(
      -1,
      () =>
        mode === "build"
          ? api.farmBuild(id, spot)
          : api.farmMoveBuilding(id, spot),
      mode === "build"
        ? `Đã xây ${placeB.name.toLowerCase()} 🔨`
        : `Đã dời ${placeB.name.toLowerCase()} sang chỗ mới`,
    );
    if (ok) setPlacing(null);
  };
  // Esc: thôi chọn chỗ
  useEffect(() => {
    if (!placing) return;
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape" && !document.querySelector("dialog[open]")) {
        e.stopImmediatePropagation();
        setPlacing(null);
      }
    };
    window.addEventListener("keydown", onKey, true);
    return () => window.removeEventListener("keydown", onKey, true);
  }, [placing]);

  /** Bấm xưởng trên bản đồ: có món xong thì lấy hết, chưa xây thì xây (đủ cấp), còn lại mở tab Xưởng. */
  const onKitchen = () => {
    if (!view?.me || busy) return;
    const k = view.buildings?.find((b) => b.id === "kitchen");
    const done = (view.jobs ?? []).filter(
      (j) => new Date(j.ready_at).getTime() <= now,
    ).length;
    setTab("kitchen");
    if (done > 0) return void act(-1, () => api.farmCraftCollect("all"));
    if (k && !k.built && view.level >= k.level)
      return startPlacing("kitchen", "build");
    if (k && !k.built)
      return showToast(
        `Lên cấp ${k.level} để xây xưởng chế biến: biến rau củ, trứng, sữa thành món giá cao.`,
      );
    scrollToPanel();
  };

  const tod = timeOfDay(new Date(now));
  const ripeCount = view
    ? view.plots.filter((p) => liveState(p, now).state === "ripe").length
    : 0;
  const dogLeft = view?.dog_until
    ? new Date(view.dog_until).getTime() - now
    : 0;

  return (
    <div
      className={`farm tod-${tod}`}
      role="dialog"
      aria-modal="true"
      aria-label="Nông trại"
    >
      <div className="farm-scroll">
        <header className="farm-hud">
          <button
            type="button"
            className="farm-back"
            onClick={() => farmScreen.close()}
            aria-label="Về khung chat"
          >
            <Icon name="arrowLeft" size={18} />
            <span>Về chat</span>
          </button>
          {view && (
            <div className={`farm-owner${view.me ? " is-mine" : ""}`}>
              {/* vườn của mình: chỉ cấp độ + XP; đi thăm: avatar, tên, cấp của chủ vườn */}
              {view.me ? (
                <LevelBar view={view} />
              ) : (
                <>
                  <Avatar user={view.user} size={30} />
                  <span className="farm-owner-name">{displayName(view.user)}</span>
                  <span className="farm-level-badge">Cấp {view.level}</span>
                </>
              )}
            </div>
          )}
          <div className="farm-hud-right">
            {mine?.wallet && (
              <span className="farm-coins" title="Số xu của bạn">
                <span aria-hidden><Emo e="🪙" /></span>
                {mine.wallet.balance.toLocaleString("vi-VN")}
                {mine.wallet.debt > 0 && (
                  <small title="Đang nợ: 50% xu kiếm được tự trả nợ">
                    nợ {mine.wallet.debt}
                  </small>
                )}
              </span>
            )}
            {visit && (
              <button
                type="button"
                className="farm-home"
                onClick={() => setVisit(null)}
                aria-label="Về vườn của tôi"
              >
                <Emo e="🏠" /> <span>Về nhà</span>
              </button>
            )}
          </div>
        </header>

        <div className="farm-body">
          <section className="farm-field" aria-label="Cánh đồng">
            <Scenery tod={tod} dog={dogLeft > 0} />
            {view ? (
              <>
                {placing && view.me && placeB ? (
                  <div className="farm-place-bar" role="toolbar">
                    <span className={placeProblem ? "is-bad" : ""}>
                      {placeProblem ? (
                        <>
                          <Emo e="🚧" /> {placeProblem}
                        </>
                      ) : (
                        <>
                          <Emo e="👆" /> Kéo hoặc chạm để chọn chỗ đặt{" "}
                          {placeB.name.toLowerCase()}
                        </>
                      )}
                    </span>
                    <div>
                      <button
                        type="button"
                        className="btn btn-sm"
                        onClick={() => setPlacing(null)}
                      >
                        Huỷ
                      </button>
                      <button
                        type="button"
                        className="btn btn-sm btn-primary"
                        disabled={!!placeProblem || busy}
                        onClick={() => void confirmPlace()}
                      >
                        {placing.mode === "build" ? (
                          <>
                            Xây ở đây · {placeB.price} <Emo e="🪙" />
                          </>
                        ) : (
                          "Đặt ở đây"
                        )}
                      </button>
                    </div>
                  </div>
                ) : (
                <div className="farm-banner">
                  {view.me ? (
                    ripeCount > 0 ? (
                      <button
                        type="button"
                        className="farm-harvest-all"
                        disabled={busy}
                        onClick={() =>
                          void act(null, () => api.farmHarvest("all"))
                        }
                      >
                        <Emo e="🧺" /> Thu hoạch tất cả <b>{ripeCount}</b>
                      </button>
                    ) : (
                      <span className="farm-tip">{fieldTip(view, now)}</span>
                    )
                  ) : (
                    <span className="farm-tip is-visit">
                      <Emo e="🫣" />
                      {view.steals_left !== undefined && (
                        <>
                          Còn <b>{view.steals_left}</b> lượt trộm ·{" "}
                        </>
                      )}
                      ô chín: trộm · ô đang lớn: tưới giúp
                    </span>
                  )}
                  {!view.me && view.couple_state && (
                    <CoupleButton
                      state={view.couple_state}
                      busy={busy}
                      onPropose={() =>
                        void act(
                          -1,
                          () => api.farmPropose(view.user.id),
                          `Đã ngỏ lời với ${displayName(view.user)} 💌 Chờ người ta trả lời nhé.`,
                        )
                      }
                      onAccept={() =>
                        void act(
                          -1,
                          () => api.farmCoupleRespond(view.user.id, true),
                          `💞 Bạn và ${displayName(view.user)} đã thành một đôi!`,
                        )
                      }
                      onGift={() => setGiftOpen(true)}
                    />
                  )}
                </div>
                )}
                <FarmWorld
                  view={view}
                  now={now}
                  busy={busy}
                  placing={
                    placing && view.me && placeB?.size
                      ? {
                          id: placing.id,
                          spot: placing.spot,
                          ok: !placeProblem,
                          size: placeB.size,
                        }
                      : null
                  }
                  onPlaceSpot={(spot) =>
                    setPlacing((p) => (p ? { ...p, spot } : p))
                  }
                  dog={dogLeft > 0}
                  live={(p) => liveState(p, now)}
                  onPlot={onPlot}
                  onAnimal={onAnimal}
                  onBuild={(id) => {
                    const b = view.buildings?.find((x) => x.id === id);
                    setConfirm({
                      emoji: id === "coop" ? "🛖" : "🌿",
                      title: `Xây ${b?.name.toLowerCase() ?? "chuồng"}?`,
                      text:
                        id === "coop"
                          ? "Chuồng gà nuôi được 4 con, gà ăn rau muống và đẻ trứng."
                          : "Đồng cỏ nuôi được 3 con bò, cừu, dê hoặc heo.",
                      price: b?.price ?? 0,
                      need: b?.level,
                      run: () =>
                        void act(
                          -1,
                          () => api.farmBuild(id),
                          id === "coop"
                            ? "Đã xây chuồng gà 🐓"
                            : "Đã xây đồng cỏ 🐄",
                        ),
                    });
                  }}
                  onExpand={() => {
                    const n = view.expansion?.next;
                    if (!n) return;
                    setConfirm({
                      emoji: "🌱",
                      title: `Mở rộng ruộng lên ${n.plots} ô?`,
                      text: `Thêm ${n.plots - view.plots.length} ô đất để trồng cùng lúc nhiều cây hơn.`,
                      price: n.price,
                      need: n.level,
                      run: () =>
                        void act(
                          -1,
                          () => api.farmExpand(),
                          "Đã mở rộng ruộng 🌱",
                        ),
                    });
                  }}
                  onStorage={() => openTab("storage")}
                  onOrders={() => openTab("orders")}
                  onKitchen={onKitchen}
                />
                <div className="farm-pops" aria-hidden>
                  {pops.map((x) => (
                    <span key={x.id} className={`farm-pop is-${x.tone}`}>
                      {emo(x.text)}
                    </span>
                  ))}
                </div>
              </>
            ) : (
              <div className="farm-loading">
                {error ? <p>{error}</p> : <p>Đang ra đồng…</p>}
              </div>
            )}
            {toast && (
              <div className={`farm-toast is-${toast.tone}`} role="status">
                {emo(toast.text)}
              </div>
            )}
            {error && view && (
              <div
                className="farm-toast is-bad"
                role="alert"
                onClick={() => setError(null)}
              >
                {error}
              </div>
            )}
          </section>

          <aside className="farm-panel">
            <nav
              className="farm-tabs"
              role="tablist"
              aria-label="Mục nông trại"
            >
              {TABS.map((t) => (
                <button
                  key={t.id}
                  type="button"
                  role="tab"
                  aria-selected={tab === t.id}
                  className="farm-tab"
                  onClick={() => {
                    setTab(t.id);
                    if (t.id === "log") setNewLogs(0);
                  }}
                >
                  <span aria-hidden>
                    <Emo e={t.icon} />
                  </span>
                  <span>{t.label}</span>
                  {t.id === "log" && newLogs > 0 && (
                    <i className="farm-tab-dot" />
                  )}
                  {t.id === "quests" && mine && questsReady(mine) > 0 && (
                    <i className="farm-tab-count">{questsReady(mine)}</i>
                  )}
                  {t.id === "kitchen" && mine && jobsReady(mine, now) > 0 && (
                    <i className="farm-tab-count">{jobsReady(mine, now)}</i>
                  )}
                </button>
              ))}
            </nav>
            <div className="farm-panel-body" role="tabpanel">
              {tab === "quests" && mine && (
                <QuestsPanel
                  farm={mine}
                  busy={busy}
                  onClaim={(id) => void act(-1, () => api.farmClaim(id))}
                />
              )}
              {tab === "neighbors" && (
                <NeighborsPanel
                  farm={mine}
                  busy={busy}
                  onGift={() => setGiftOpen(true)}
                  onRespond={(id, accept) =>
                    void act(
                      -1,
                      () => api.farmCoupleRespond(id, accept),
                      accept ? "💞 Hai bạn đã thành một đôi!" : "Đã từ chối lời ngỏ.",
                    )
                  }
                  onCancelPropose={() =>
                    void act(-1, () => api.farmCancelPropose(), "Đã rút lại lời ngỏ.")
                  }
                  onBreakUp={() =>
                    void act(-1, () => api.farmBreakUp(), "Đã chia tay 💔")
                  }
                  onVisit={goVisit}
                  onHome={() => setVisit(null)}
                  me={mine?.user ?? null}
                  myLevel={mine?.level ?? 1}
                  visiting={visit?.user.id ?? null}
                />
              )}
              {tab === "orders" && mine && (
                <OrdersPanel
                  farm={mine}
                  now={now}
                  busy={busy}
                  onDeliver={(id) =>
                    void act(
                      -1,
                      () => api.farmDeliver(id),
                      "Xe tải đã chở hàng đi 🚚",
                    )
                  }
                  onDiscard={(id) =>
                    void act(
                      -1,
                      () => api.farmDiscard(id),
                      "Đã huỷ đơn, đơn mới sẽ tới sau ít phút.",
                    )
                  }
                />
              )}
              {tab === "storage" && mine && (
                <StoragePanel
                  farm={mine}
                  busy={busy}
                  onSell={(item, qty) =>
                    void act(-1, () => api.farmSell(item, qty))
                  }
                  onBuy={(item, qty) =>
                    void act(-1, () => api.farmBuyItem(item, qty))
                  }
                  onUpgrade={() =>
                    void act(
                      -1,
                      () => api.farmUpgradeStorage(),
                      "Kho đã rộng hơn 📦",
                    )
                  }
                />
              )}
              {tab === "kitchen" && mine && (
                <KitchenPanel
                  farm={mine}
                  now={now}
                  busy={busy}
                  onBuild={() => startPlacing("kitchen", "build")}
                  onMove={() => startPlacing("kitchen", "move")}
                  onUpgrade={() =>
                    void act(
                      -1,
                      () => api.farmUpgradeBuilding("kitchen"),
                      "Xưởng có thêm lò 🔥",
                    )
                  }
                  onCraft={(r) =>
                    void act(
                      -1,
                      () => api.farmCraft(r.id),
                      `Đang làm ${r.name.toLowerCase()} ${r.emoji}`,
                    )
                  }
                  onCollect={(slot) =>
                    void act(-1, () => api.farmCraftCollect(slot))
                  }
                />
              )}
              {tab === "shop" && mine && (
                <ShopPanel
                  farm={mine}
                  now={now}
                  busy={busy}
                  onBuild={(id) =>
                    id === "kitchen"
                      ? startPlacing(id, "build")
                      : void act(-1, () => api.farmBuild(id), "Đã xây xong 🔨")
                  }
                  onUpgradeBuilding={(id) =>
                    void act(
                      -1,
                      () => api.farmUpgradeBuilding(id),
                      id === "kitchen"
                        ? "Xưởng có thêm lò 🔥"
                        : "Chuồng đã rộng hơn 🔨",
                    )
                  }
                  onBuyAnimal={(k) =>
                    void act(
                      -1,
                      () => api.farmBuyAnimal(k),
                      "Vật nuôi mới đã về chuồng 🐾",
                    )
                  }
                  onBuyPlot={() =>
                    void act(
                      -1,
                      () =>
                        mine.expansion ? api.farmExpand() : api.farmBuyPlot(),
                      "Đã mở rộng ruộng 🌱",
                    )
                  }
                  onBuyDog={() =>
                    void act(
                      -1,
                      () => api.farmBuyDog(),
                      "Chó giữ nhà đã về canh vườn 🐕",
                    )
                  }
                />
              )}
              {tab === "log" && mine && (
                <LogPanel farm={mine} now={now} onVisit={goVisit} />
              )}
              {tab === "top" && (
                <LeaderPanel
                  meId={state.me.id}
                  onVisit={(id) =>
                    id === state.me.id ? setVisit(null) : void goVisit(id)
                  }
                />
              )}
              {!mine && tab !== "neighbors" && tab !== "top" && (
                <p className="muted">Đang tải…</p>
              )}
            </div>
          </aside>
        </div>
      </div>

      {giftOpen && mine?.couple && (
        <GiftDialog
          farm={mine}
          busy={busy}
          onClose={() => setGiftOpen(false)}
          onGift={async (item, qty) => {
            const ok = await act(
              -1,
              () => api.farmGift(item, qty),
              `Đã gửi quà cho ${displayName(mine.couple!.partner)} 🎁`,
            );
            if (ok) setGiftOpen(false);
          }}
        />
      )}
      {confirm && mine && (
        <ConfirmBuyDialog
          c={confirm}
          balance={mine.wallet?.balance ?? 0}
          level={mine.level}
          onClose={() => setConfirm(null)}
        />
      )}
      {seedFor !== null && mine && (
        <SeedPicker
          farm={mine}
          plot={seedFor}
          onClose={() => setSeedFor(null)}
          onPlant={async (crop, all) => {
            const targets = all
              ? mine.plots
                  .filter((p) => liveState(p, now).state === "empty")
                  .map((p) => p.index)
              : [seedFor];
            setSeedFor(null);
            for (const idx of targets) {
              await act(idx, () => api.farmPlant(idx, crop.id));
            }
          }}
        />
      )}
    </div>
  );
}

type ConfirmBuy = {
  emoji: string;
  title: string;
  text: string;
  price: number;
  /** cấp nông trại cần (không đủ thì không cho mua) */
  need?: number;
  run: () => void;
};

/** Hỏi lại trước khi tiêu xu cho một thứ bấm từ bản đồ (mở rộng ruộng, xây chuồng, xưởng). */
function ConfirmBuyDialog({
  c,
  balance,
  level,
  onClose,
}: {
  c: ConfirmBuy;
  balance: number;
  level: number;
  onClose: () => void;
}) {
  const block =
    c.need && level < c.need
      ? `Cần đạt cấp ${c.need}`
      : balance < c.price
        ? `Thiếu ${(c.price - balance).toLocaleString("vi-VN")} xu`
        : null;
  return (
    <Dialog title={c.title} onClose={onClose} className="farm-confirm">
      <div className="farm-confirm-body">
        <span className="farm-confirm-icon" aria-hidden>
          <Emo e={c.emoji} />
        </span>
        <p>{c.text}</p>
        <p className="farm-confirm-price">
          Giá <b>{c.price.toLocaleString("vi-VN")}</b> <Emo e="🪙" />
          <small>
            {" "}
            · đang có {balance.toLocaleString("vi-VN")} <Emo e="🪙" />
          </small>
        </p>
      </div>
      <div className="farm-confirm-actions">
        <button type="button" className="btn" onClick={onClose}>
          Để sau
        </button>
        <button
          type="button"
          className="btn btn-primary"
          disabled={!!block}
          onClick={() => {
            onClose();
            c.run();
          }}
        >
          {block ?? emo(`Đồng ý · ${c.price.toLocaleString("vi-VN")} 🪙`)}
        </button>
      </div>
    </Dialog>
  );
}

/* =========================================================
   Cảnh vật: trời theo giờ, mặt trời / trăng, mây, đồi, hàng rào
   ========================================================= */

function Scenery({ tod }: { tod: string; dog: boolean }) {
  // ánh sáng theo giờ phủ lên cả thế giới (bình minh cam, hoàng hôn tím, đêm xanh thẫm)
  return <div className={`farm-tint is-${tod}`} aria-hidden />;
}

function plotHint(p: Plot, st: PlotState, me: boolean, view: FarmView): string {
  if (st === "ripe" && !me) {
    if (p.steals >= 2) return "Vụ này bị trộm đủ rồi, tha cho người ta đi 😅";
    if ((view.steals_left ?? 1) <= 0) return "Hôm nay bạn hết lượt trộm rồi.";
    return "Bạn đã trộm vụ này rồi.";
  }
  if (st === "growing" && !me)
    return p.waters >= 3
      ? "Cây này được tưới đủ rồi."
      : "Bạn đã tưới cây này rồi.";
  if (st === "withered") return "Cây héo mất rồi.";
  return "Ô đất trống.";
}

function fieldTip(view: FarmView, now: number): ReactNode {
  const growing = view.plots
    .map((p) => liveState(p, now))
    .filter((l) => l.state === "growing")
    .sort((a, b) => a.left - b.left);
  const empty = view.plots.filter(
    (p) => liveState(p, now).state === "empty",
  ).length;
  if (growing.length > 0)
    return (
      <>
        <Emo e="⏱️" /> Chín sau <b>{formatLeft(growing[0].left)}</b>
        {empty > 0 && <> · {empty} ô trống</>}
      </>
    );
  if (empty > 0)
    return (
      <>
        <Emo e="🌱" /> Chạm ô đất để gieo hạt
      </>
    );
  return "Ruộng đầy, chờ cây lớn nhé";
}

/* =========================================================
   Thanh cấp độ
   ========================================================= */

function LevelBar({ view }: { view: FarmView }) {
  const span = view.next_level_xp ? view.next_level_xp - view.level_xp : 1;
  const pct = view.next_level_xp
    ? Math.min(100, ((view.xp - view.level_xp) / span) * 100)
    : 100;
  return (
    <div
      className="farm-level"
      title={
        view.next_level_xp
          ? `${view.xp}/${view.next_level_xp} XP`
          : "Cấp tối đa"
      }
    >
      <span className="farm-level-badge">Cấp {view.level}</span>
      <span className="farm-level-bar">
        <i style={{ width: `${pct}%` }} />
      </span>
      <span className="farm-level-xp">
        {view.next_level_xp ? `${view.xp}/${view.next_level_xp} XP` : "Tối đa"}
      </span>
    </div>
  );
}

/* =========================================================
   Chọn hạt giống
   ========================================================= */

function SeedPicker({
  farm,
  plot,
  onClose,
  onPlant,
}: {
  farm: FarmView;
  plot: number;
  onClose: () => void;
  onPlant: (c: Crop, all: boolean) => void;
}) {
  const balance = farm.wallet?.balance ?? 0;
  const empty = farm.plots.filter((p) => p.state === "empty").length;
  const [all, setAll] = useState(false);
  const crops = [...farm.crops].sort(
    (a, b) => a.level - b.level || a.seed - b.seed,
  );
  return (
    <Dialog
      title={`Gieo hạt vào ô ${plot + 1}`}
      onClose={onClose}
      wide
      className="farm-seed-dialog"
    >
      <ul className="seed-grid">
        {crops.map((c) => {
          const locked = c.level > farm.level;
          const cost = c.seed * (all ? empty : 1);
          const poor = !locked && cost > balance;
          return (
            <li key={c.id}>
              <button
                type="button"
                className={`seed-card${locked ? " is-locked" : ""}`}
                disabled={locked || poor}
                onClick={() => onPlant(c, all)}
              >
                <span className="seed-emoji" aria-hidden>
                  <Emo e={locked ? "🔒" : c.emoji} />
                </span>
                <b>{c.name}</b>
                <span className="seed-time"><Emo e="⏱" /> {formatGrow(c.grow_sec)}</span>
                {locked ? (
                  <span className="seed-lock">Mở ở cấp {c.level}</span>
                ) : (
                  <span className="seed-money">
                    <span>{c.seed} <Emo e="🪙" /></span>
                    <span aria-hidden>→</span>
                    <span className="seed-sell">{c.sell} <Emo e="🪙" /></span>
                  </span>
                )}
                {!locked && (
                  <span className="seed-profit">
                    lời {c.sell - c.seed} · +{c.xp} XP
                  </span>
                )}
                {poor && <span className="seed-poor">Thiếu xu</span>}
              </button>
            </li>
          );
        })}
      </ul>
      {empty > 1 && (
        <label className="check seed-all">
          <input
            type="checkbox"
            checked={all}
            onChange={(e) => setAll(e.target.checked)}
          />
          Gieo cùng loại vào cả {empty} ô trống
        </label>
      )}
      <p className="seed-note">
        Đang có <b>{balance.toLocaleString("vi-VN")} <Emo e="🪙" /></b>. Cây chín để quá 12
        giờ sẽ héo, nhớ ghé thu hoạch nhé.
      </p>
    </Dialog>
  );
}

/* =========================================================
   Nhiệm vụ
   ========================================================= */

const QUEST_ICON: Record<string, string> = {
  harvest: "🧺",
  plant: "🌱",
  plant_rare: "🌽",
  water: "💧",
  bug: "🐛",
  steal: "🫣",
  earn: "🪙",
  visit: "🏡",
};

/** Số lò của xưởng đã nấu xong, chờ lấy. */
const jobsReady = (f: FarmView, now: number) =>
  (f.jobs ?? []).filter((j) => new Date(j.ready_at).getTime() <= now).length;

const questsReady = (f: FarmView) =>
  (f.quests ?? []).filter((q) => q.done && !q.claimed).length +
  (f.chest?.ready && !f.chest.claimed ? 1 : 0);

function QuestsPanel({
  farm,
  busy,
  onClaim,
}: {
  farm: FarmView;
  busy: boolean;
  onClaim: (id: string) => void;
}) {
  const quests = farm.quests ?? [];
  const doneCount = quests.filter((q) => q.done).length;
  return (
    <div className="farm-section">
      <DailyCheckin />
      <h3 className="farm-h">Nhiệm vụ hôm nay</h3>
      <ul className="quest-list">
        {quests.map((q) => (
          <QuestCard
            key={q.id}
            q={q}
            busy={busy}
            onClaim={() => onClaim(q.id)}
          />
        ))}
      </ul>
      {farm.chest && (
        <div
          className={`quest-chest${farm.chest.ready && !farm.chest.claimed ? " is-ready" : ""}`}
        >
          <span className="quest-chest-icon" aria-hidden>
            <Emo e={farm.chest.claimed ? "📭" : "🎁"} />
          </span>
          <div>
            <b>Rương thưởng ngày</b>
            <span
              className="quest-dots"
              aria-label={`${doneCount}/4 nhiệm vụ xong`}
            >
              {quests.map((q) => (
                <i key={q.id} className={q.done ? "is-on" : ""} />
              ))}
            </span>
            <small>
              Xong cả 4 nhiệm vụ: +{farm.chest.xu} <Emo e="🪙" /> +{farm.chest.xp} XP
            </small>
          </div>
          {farm.chest.claimed ? (
            <span className="quest-claimed">Đã mở</span>
          ) : (
            <button
              type="button"
              className="btn btn-sm btn-primary"
              disabled={!farm.chest.ready || busy}
              onClick={() => onClaim("chest")}
            >
              Mở rương
            </button>
          )}
        </div>
      )}
      <p className="farm-foot">Nhiệm vụ đổi mới lúc 0 giờ mỗi ngày.</p>
    </div>
  );
}

function QuestCard({
  q,
  busy,
  onClaim,
}: {
  q: Quest;
  busy: boolean;
  onClaim: () => void;
}) {
  const pct = Math.min(100, (q.progress / Math.max(1, q.goal)) * 100);
  return (
    <li
      className={`quest${q.done ? " is-done" : ""}${q.claimed ? " is-claimed" : ""}`}
    >
      <span className="quest-icon" aria-hidden>
        <Emo e={QUEST_ICON[q.id] ?? "⭐"} />
      </span>
      <div className="quest-main">
        <b>{q.title}</b>
        <span className="quest-bar">
          <i style={{ width: `${pct}%` }} />
        </span>
        <small>
          {Math.min(q.progress, q.goal)}/{q.goal} · thưởng {q.xu} <Emo e="🪙" /> {q.xp} XP
        </small>
      </div>
      {q.claimed ? (
        <span className="quest-claimed">✓</span>
      ) : (
        <button
          type="button"
          className="btn btn-sm btn-primary"
          disabled={!q.done || busy}
          onClick={onClaim}
        >
          Nhận
        </button>
      )}
    </li>
  );
}

const STREAK = [100, 120, 140, 160, 180, 200, 300];

/** Điểm danh: thưởng tăng dần theo chuỗi ngày liên tục. */
function DailyCheckin() {
  const { api, dispatch } = useChat();
  const [w, setW] = useState<Wallet | null>(null);
  const [msg, setMsg] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  useEffect(() => {
    api
      .getWallet()
      .then(setW)
      .catch(() => {});
  }, [api]);
  if (!w) return null;
  const streak = w.streak ?? 0;
  const claimed = !w.can_claim;
  // hôm nay là ô thứ mấy trong vòng 7 ngày; đã nhận thì ô hôm nay tính là xong
  const todayIdx = claimed ? Math.max(0, streak - 1) % 7 : streak % 7;
  const claim = async () => {
    setBusy(true);
    setMsg(null);
    try {
      const nw = await api.claimDaily();
      const got = nw.balance - w.balance;
      setW(nw);
      dispatch({ type: "wallet.set", balance: nw.balance, debt: nw.debt });
      playCoinSound();
      setMsg(got > 0 ? `+${got} 🪙` : "Đã điểm danh! (một phần trừ nợ)");
    } catch (err) {
      setMsg(errMsg(err));
    } finally {
      setBusy(false);
    }
  };
  return (
    <div className="checkin">
      <div className="checkin-head">
        <b>Điểm danh</b>
        <small>
          {streak > 0
            ? `${streak} ngày liên tục 🔥`
            : "Điểm danh mỗi ngày, thưởng tăng dần"}
        </small>
      </div>
      <ol className="checkin-days">
        {STREAK.map((xu, i) => (
          <li
            key={i}
            className={
              i < todayIdx || (claimed && i === todayIdx)
                ? "is-done"
                : i === todayIdx
                  ? "is-today"
                  : ""
            }
          >
            <span>N{i + 1}</span>
            <b>{xu}</b>
          </li>
        ))}
      </ol>
      <div className="checkin-foot">
        <button
          type="button"
          className="btn btn-sm btn-primary"
          disabled={busy || claimed}
          onClick={() => void claim()}
        >
          {claimed ? "Hôm nay đã nhận" : `Nhận ${STREAK[todayIdx]} xu`}
        </button>
        {msg && <span className="checkin-msg">{emo(msg)}</span>}
      </div>
    </div>
  );
}

/* =========================================================
   Hàng xóm
   ========================================================= */

function NeighborsPanel({
  farm,
  busy,
  onGift,
  onRespond,
  onCancelPropose,
  onBreakUp,
  onVisit,
  onHome,
  me,
  myLevel,
  visiting,
}: {
  farm: FarmView | null;
  busy: boolean;
  onGift: () => void;
  onRespond: (id: number, accept: boolean) => void;
  onCancelPropose: () => void;
  onBreakUp: () => void;
  onVisit: (id: number) => void;
  onHome: () => void;
  me: FarmView["user"] | null;
  myLevel: number;
  visiting: number | null;
}) {
  const { api } = useChat();
  const [list, setList] = useState<Neighbor[] | null>(null);
  const [err, setErr] = useState<string | null>(null);
  useEffect(() => {
    api
      .farmNeighbors()
      .then((l) =>
        setList(
          [...l].sort(
            (a, b) =>
              Number(!!b.partner) - Number(!!a.partner) ||
              b.stealable - a.stealable ||
              b.needs_help - a.needs_help ||
              b.xp - a.xp,
          ),
        ),
      )
      .catch((e) => setErr(errMsg(e)));
    // thành đôi / chia tay thì tải lại để người thương lên đầu danh sách
  }, [api, farm?.couple?.partner.id]);
  if (err) return <p className="form-error">{err}</p>;
  if (!list) return <p className="muted">Đang tải…</p>;
  if (list.length === 0)
    return (
      <EmptyState icon="users" title="Chưa có hàng xóm">
        Mời đồng nghiệp vào Go Chat để cùng làm vườn.
      </EmptyState>
    );
  return (
    <div className="farm-section">
      {farm && (
        <LoveCard
          farm={farm}
          busy={busy}
          onGift={onGift}
          onRespond={onRespond}
          onCancelPropose={onCancelPropose}
          onBreakUp={onBreakUp}
          onVisit={onVisit}
        />
      )}
      <p className="farm-foot">
        Ghé vườn hàng xóm để tưới giúp, bắt sâu… hoặc trộm rau khi họ quên hái
        <Emo e="🫣" />
      </p>
      <ul className="neighbor-list">
        {me && (
          <li>
            <button
              type="button"
              className={`neighbor is-home${visiting === null ? " is-on" : ""}`}
              onClick={onHome}
            >
              <Avatar user={me} size={36} />
              <span className="neighbor-main">
                <b><Emo e="🏠" /> Vườn của tôi</b>
                <small>Cấp {myLevel}</small>
              </span>
            </button>
          </li>
        )}
        {list.map((n) => (
          <li key={n.user.id}>
            <button
              type="button"
              className={`neighbor${visiting === n.user.id ? " is-on" : ""}`}
              onClick={() => onVisit(n.user.id)}
            >
              <Avatar user={n.user} size={36} />
              <span className="neighbor-main">
                <b>{displayName(n.user)}</b>
                <small>
                  {n.partner && (
                    <>
                      <Emo e="💞" /> Người thương ·{" "}
                    </>
                  )}
                  Cấp {n.level}
                  {n.dog && emo(" · 🐕 có chó")}
                </small>
              </span>
              <span className="neighbor-badges">
                {n.stealable > 0 && (
                  <span className="nb is-steal" title="Ô chín có thể trộm">
                    <Emo e="🧺" /> {n.stealable}
                  </span>
                )}
                {n.needs_help > 0 && (
                  <span className="nb is-help" title="Ô cần tưới / bắt sâu">
                    <Emo e="💧" /> {n.needs_help}
                  </span>
                )}
              </span>
            </button>
          </li>
        ))}
      </ul>
    </div>
  );
}

/* =========================================================
   Kết đôi
   ========================================================= */

/** Nút trên dải gợi ý khi đi thăm vườn: ngỏ lời / nhận lời / tặng quà người thương. */
function CoupleButton({
  state,
  busy,
  onPropose,
  onAccept,
  onGift,
}: {
  state: NonNullable<FarmView["couple_state"]>;
  busy: boolean;
  onPropose: () => void;
  onAccept: () => void;
  onGift: () => void;
}) {
  if (state === "sent")
    return (
      <span className="farm-tip farm-couple-btn is-sent">
        <Emo e="💌" /> Đã ngỏ lời
      </span>
    );
  const [icon, label, run] =
    state === "partner"
      ? ["🎁", "Tặng quà", onGift]
      : state === "incoming"
        ? ["💞", "Nhận lời kết đôi", onAccept]
        : ["💌", "Ngỏ lời kết đôi", onPropose];
  return (
    <button
      type="button"
      className="farm-couple-btn"
      disabled={busy}
      onClick={run}
    >
      <Emo e={icon} /> {label}
    </button>
  );
}

/** Thẻ tình yêu đầu tab Hàng xóm: cặp đôi + độ thân mật, lời ngỏ đến / đi, hoặc cách kết đôi. */
function LoveCard({
  farm,
  busy,
  onGift,
  onRespond,
  onCancelPropose,
  onBreakUp,
  onVisit,
}: {
  farm: FarmView;
  busy: boolean;
  onGift: () => void;
  onRespond: (id: number, accept: boolean) => void;
  onCancelPropose: () => void;
  onBreakUp: () => void;
  onVisit: (id: number) => void;
}) {
  const [sure, setSure] = useState(false);
  useEffect(() => {
    if (!sure) return;
    const t = window.setTimeout(() => setSure(false), 4000);
    return () => window.clearTimeout(t);
  }, [sure]);
  const c = farm.couple;
  const reqs = farm.couple_requests ?? [];
  if (c) {
    const span = (c.next_love ?? c.love) - c.level_love;
    const pct = c.next_love
      ? Math.min(100, ((c.love - c.level_love) / Math.max(1, span)) * 100)
      : 100;
    return (
      <div className="love-card">
        <div className="love-pair">
          <Avatar user={farm.user} size={40} />
          <span className="love-heart" aria-hidden>
            <Emo e="💞" />
          </span>
          <Avatar user={c.partner} size={40} />
        </div>
        <b className="love-names">
          Bạn & {displayName(c.partner)}
        </b>
        <span className="love-level">
          Cấp {c.level} · {c.level_name}
        </span>
        <span className="quest-bar love-bar" title={`${c.love} điểm thân mật`}>
          <i style={{ width: `${pct}%` }} />
        </span>
        <small className="love-hint">
          {c.next_love
            ? `${c.love}/${c.next_love} thân mật · lên cấp cả hai nhận ${30 * (c.level + 1)} xu`
            : `${c.love} thân mật · cấp cao nhất rồi 💍`}
        </small>
        <small className="love-hint">
          Tưới giúp +2 · bắt sâu +3 · ghé vườn +2/ngày · tặng quà (còn{" "}
          {c.gifts_left ?? 0} lần hôm nay)
        </small>
        <div className="love-actions">
          <button
            type="button"
            className="btn btn-sm btn-primary"
            onClick={() => onVisit(c.partner.id)}
          >
            Ghé vườn
          </button>
          <button
            type="button"
            className="btn btn-sm"
            disabled={busy || !c.gifts_left}
            onClick={onGift}
          >
            <Emo e="🎁" /> Tặng quà
          </button>
          <button
            type="button"
            className={`btn btn-sm${sure ? " btn-danger" : ""}`}
            disabled={busy}
            onClick={() => (sure ? onBreakUp() : setSure(true))}
          >
            {sure ? "Chắc chưa? Bấm lần nữa" : "Chia tay"}
          </button>
        </div>
      </div>
    );
  }
  return (
    <div className="love-card is-single">
      {reqs.map((r) => (
        <div key={r.user.id} className="love-request">
          <Avatar user={r.user} size={34} />
          <span>
            <b>{displayName(r.user)}</b> ngỏ lời kết đôi với bạn <Emo e="💌" />
          </span>
          <div className="love-actions">
            <button
              type="button"
              className="btn btn-sm btn-primary"
              disabled={busy}
              onClick={() => onRespond(r.user.id, true)}
            >
              Nhận lời
            </button>
            <button
              type="button"
              className="btn btn-sm"
              disabled={busy}
              onClick={() => onRespond(r.user.id, false)}
            >
              Từ chối
            </button>
          </div>
        </div>
      ))}
      {farm.couple_sent && (
        <div className="love-request">
          <Avatar user={farm.couple_sent.user} size={34} />
          <span>
            Đang chờ <b>{displayName(farm.couple_sent.user)}</b> trả lời{" "}
            <Emo e="💌" />
          </span>
          <button
            type="button"
            className="btn btn-sm"
            disabled={busy}
            onClick={onCancelPropose}
          >
            Rút lời
          </button>
        </div>
      )}
      {reqs.length === 0 && !farm.couple_sent && (
        <p className="love-empty">
          <Emo e="💞" /> <b>Kết đôi:</b> ghé vườn một người rồi bấm{" "}
          <b>Ngỏ lời</b>. Chăm vườn, tặng quà cho người thương để tăng thân
          mật, lên cấp cả hai cùng nhận xu.
        </p>
      )}
    </div>
  );
}

/** Tặng nông sản / sản phẩm trong kho cho người thương. */
function GiftDialog({
  farm,
  busy,
  onClose,
  onGift,
}: {
  farm: FarmView;
  busy: boolean;
  onClose: () => void;
  onGift: (item: string, qty: number) => void;
}) {
  const stock = farm.storage?.stock ?? {};
  const items = (farm.items ?? []).filter((i) => (stock[i.id] ?? 0) > 0);
  const [pick, setPick] = useState<string | null>(items[0]?.id ?? null);
  const [qty, setQty] = useState(1);
  const it = items.find((i) => i.id === pick);
  const have = it ? (stock[it.id] ?? 0) : 0;
  const n = Math.min(qty, have, 10);
  const love = it ? Math.min(10, Math.max(1, Math.ceil((it.price * n) / 20))) : 0;
  const partner = farm.couple?.partner;
  return (
    <Dialog
      title={`Tặng quà cho ${partner ? displayName(partner) : "người thương"}`}
      onClose={onClose}
      className="farm-gift-dialog"
    >
      {items.length === 0 ? (
        <p className="muted">
          Kho đang trống. Thu hoạch hoặc nấu món ở xưởng rồi quay lại tặng nhé.
        </p>
      ) : (
        <>
          <ul className="gift-grid">
            {items.map((i) => (
              <li key={i.id}>
                <button
                  type="button"
                  className={`gift-item${pick === i.id ? " is-on" : ""}`}
                  onClick={() => {
                    setPick(i.id);
                    setQty((q) => Math.min(q, stock[i.id] ?? 1));
                  }}
                  title={i.name}
                >
                  <Emo e={i.emoji} />
                  <small>×{stock[i.id]}</small>
                </button>
              </li>
            ))}
          </ul>
          {it && (
            <div className="gift-qty">
              <b>{it.name}</b>
              <div className="gift-qty-btns">
                {[1, 5, 10].map((q) => (
                  <button
                    key={q}
                    type="button"
                    className={`btn btn-sm${n === Math.min(q, have) && qty === q ? " btn-primary" : ""}`}
                    disabled={q > have && q !== 1}
                    onClick={() => setQty(q)}
                  >
                    {q}
                  </button>
                ))}
              </div>
            </div>
          )}
          <p className="gift-note">
            +{love} <Emo e="💗" /> thân mật · còn {farm.couple?.gifts_left ?? 0}{" "}
            lần tặng hôm nay
          </p>
          <div className="farm-confirm-actions">
            <button type="button" className="btn" onClick={onClose}>
              Để sau
            </button>
            <button
              type="button"
              className="btn btn-primary"
              disabled={busy || !it || n < 1 || !farm.couple?.gifts_left}
              onClick={() => it && onGift(it.id, n)}
            >
              Tặng {n} <Emo e={it?.emoji} />
            </button>
          </div>
        </>
      )}
    </Dialog>
  );
}

/* =========================================================
   Cửa hàng
   ========================================================= */

function ShopPanel({
  farm,
  now,
  busy,
  onBuyPlot,
  onBuyDog,
  onBuild,
  onUpgradeBuilding,
  onBuyAnimal,
}: {
  farm: FarmView;
  now: number;
  busy: boolean;
  onBuyPlot: () => void;
  onBuyDog: () => void;
  onBuild: (id: string) => void;
  onUpgradeBuilding: (id: string) => void;
  onBuyAnimal: (kind: string) => void;
}) {
  const balance = farm.wallet?.balance ?? 0;
  const exp = farm.expansion?.next;
  const np = exp ? { price: exp.price, level: exp.level } : farm.next_plot;
  const animals = farm.animals ?? [];
  const items = new Map((farm.items ?? []).map((i) => [i.id, i]));
  const dogLeft = farm.dog_until ? new Date(farm.dog_until).getTime() - now : 0;
  const plotBlock = !np
    ? "Đã mở hết ô đất"
    : farm.level < np.level
      ? `Cần cấp ${np.level}`
      : balance < np.price
        ? "Thiếu xu"
        : null;
  return (
    <div className="farm-section">
      <div className="shop-card">
        <span className="shop-icon" aria-hidden>
          <Emo e="🟫" />
        </span>
        <div>
          <b>Mở rộng ruộng</b>
          <small>
            Đang có {farm.plot_count}/{farm.max_plots} ô
            {emo(
              exp
                ? ` · lên ${exp.plots} ô: ${exp.price} 🪙, cần cấp ${exp.level}`
                : np && ` · ô tiếp theo ${np.price} 🪙, cần cấp ${np.level}`,
            )}
          </small>
        </div>
        <button
          type="button"
          className="btn btn-sm btn-primary"
          disabled={!!plotBlock || busy}
          onClick={onBuyPlot}
        >
          {emo(plotBlock ?? `Mua ${np?.price} 🪙`)}
        </button>
      </div>
      {(farm.buildings ?? []).map((b) => {
        const count = animals.filter(
          (a) =>
            farm.animal_kinds?.find((k) => k.id === a.kind)?.building === b.id,
        ).length;
        const block = !b.built
          ? farm.level < b.level
            ? `Cần cấp ${b.level}`
            : balance < b.price
              ? "Thiếu xu"
              : null
          : null;
        return (
          <div key={b.id} className="shop-card">
            <span className="shop-icon" aria-hidden>
              <Emo
                e={b.id === "coop" ? "🛖" : b.id === "kitchen" ? "🍳" : "🌿"}
              />
            </span>
            <div>
              <b>{b.name}</b>
              <small>
                {emo(
                  b.id === "kitchen"
                    ? b.built
                      ? `${b.cap} lò nấu${b.upgrade_price ? ` · thêm 2 lò ${b.upgrade_price} 🪙` : ""}`
                      : `Nấu món giá cao từ nông sản, trứng, sữa · cần cấp ${b.level}`
                    : b.built
                      ? `Đang nuôi ${count}/${b.cap} con${b.upgrade_price ? ` · nới chuồng ${b.upgrade_price} 🪙` : ""}`
                      : `Xây để nuôi ${b.id === "coop" ? "gà" : "bò, dê, cừu, heo"} · cần cấp ${b.level}`,
                )}
              </small>
            </div>
            {b.built ? (
              b.upgrade_price ? (
                <button
                  type="button"
                  className="btn btn-sm"
                  disabled={busy || balance < b.upgrade_price}
                  onClick={() => onUpgradeBuilding(b.id)}
                >
                  {b.id === "kitchen" ? "+2 lò" : "Nới"} · {b.upgrade_price}{" "}
                  <Emo e="🪙" />
                </button>
              ) : (
                <span className="quest-claimed">Tối đa</span>
              )
            ) : (
              <button
                type="button"
                className="btn btn-sm btn-primary"
                disabled={!!block || busy}
                onClick={() => onBuild(b.id)}
              >
                {emo(block ?? `Xây · ${b.price} 🪙`)}
              </button>
            )}
          </div>
        );
      })}

      {(farm.animal_kinds?.length ?? 0) > 0 && (
        <h3 className="farm-h">Vật nuôi</h3>
      )}
      <ul className="animal-shop">
        {(farm.animal_kinds ?? []).map((k) => {
          const b = farm.buildings?.find((x) => x.id === k.building);
          const count = animals.filter(
            (a) =>
              farm.animal_kinds?.find((x) => x.id === a.kind)?.building ===
              k.building,
          ).length;
          const block =
            farm.level < k.level
              ? `Cấp ${k.level}`
              : !b?.built
                ? `Cần ${b?.name.toLowerCase() ?? "chuồng"}`
                : count >= (b?.cap ?? 0)
                  ? "Chuồng đầy"
                  : balance < k.price
                    ? "Thiếu xu"
                    : null;
          const feed = items.get(k.feed);
          const prod = items.get(k.product);
          return (
            <li
              key={k.id}
              className={`animal-card${farm.level < k.level ? " is-locked" : ""}`}
            >
              <span className="animal-emoji" aria-hidden>
                <Emo e={k.emoji} />
              </span>
              <div>
                <b>{k.name}</b>
                <small>
                  Ăn {k.feed_qty} <Emo e={feed?.emoji} />{" "}
                  {feed?.name.toLowerCase() ?? k.feed} → <Emo e={prod?.emoji} />{" "}
                  {prod?.name.toLowerCase()} sau {formatGrow(k.time_sec)}
                </small>
              </div>
              <button
                type="button"
                className="btn btn-sm btn-primary"
                disabled={!!block || busy}
                onClick={() => onBuyAnimal(k.id)}
              >
                {emo(block ?? `${k.price} 🪙`)}
              </button>
            </li>
          );
        })}
      </ul>

      <div className="shop-card">
        <span className="shop-icon" aria-hidden>
          <Emo e="🐕" />
        </span>
        <div>
          <b>Chó giữ nhà</b>
          <small>
            Đuổi kẻ trộm 50% số lần, bắt nộp phạt 10 <Emo e="🪙" /> cho bạn.
            {dogLeft > 0
              ? ` Còn canh ${formatLeft(dogLeft)}.`
              : " 24 giờ / lần mua."}
          </small>
        </div>
        <button
          type="button"
          className="btn btn-sm btn-primary"
          disabled={busy || balance < 150 || dogLeft > 48 * 3600_000}
          onClick={onBuyDog}
        >
          {emo(dogLeft > 0 ? "Thêm 24h · 150 🪙" : "Mua · 150 🪙")}
        </button>
      </div>

      <h3 className="farm-h">Bảng giá hạt giống</h3>
      <table className="crop-table">
        <thead>
          <tr>
            <th>Cây</th>
            <th>Thời gian</th>
            <th>Lời</th>
            <th>Cấp</th>
          </tr>
        </thead>
        <tbody>
          {[...farm.crops]
            .sort((a, b) => a.level - b.level || a.seed - b.seed)
            .map((c) => (
              <tr
                key={c.id}
                className={c.level > farm.level ? "is-locked" : ""}
              >
                <td>
                  <Emo e={c.emoji} /> {c.name}
                </td>
                <td>{formatGrow(c.grow_sec)}</td>
                <td>+{c.sell - c.seed}</td>
                <td>{c.level}</td>
              </tr>
            ))}
        </tbody>
      </table>
    </div>
  );
}

/* =========================================================
   Xưởng chế biến
   ========================================================= */

function KitchenPanel({
  farm,
  now,
  busy,
  onBuild,
  onMove,
  onUpgrade,
  onCraft,
  onCollect,
}: {
  farm: FarmView;
  now: number;
  busy: boolean;
  onBuild: () => void;
  onMove: () => void;
  onUpgrade: () => void;
  onCraft: (r: Recipe) => void;
  onCollect: (slot: number | "all") => void;
}) {
  const k = farm.buildings?.find((b) => b.id === "kitchen");
  const recipes = farm.recipes ?? [];
  if (!k || recipes.length === 0)
    return (
      <EmptyState icon="clock" title="Xưởng chưa mở">
        Xưởng chế biến sẽ có sau khi máy chủ cập nhật.
      </EmptyState>
    );
  const balance = farm.wallet?.balance ?? 0;
  const stock = farm.storage?.stock ?? {};
  const items = new Map((farm.items ?? []).map((i) => [i.id, i]));
  const byRecipe = new Map(recipes.map((r) => [r.id, r]));
  const jobs = new Map((farm.jobs ?? []).map((j) => [j.slot, j]));
  const free = k.built ? k.cap - jobs.size : 0;
  const done = jobsReady(farm, now);
  const sorted = [...recipes].sort(
    (a, b) =>
      Number(a.level > farm.level) - Number(b.level > farm.level) ||
      a.level - b.level ||
      a.time_sec - b.time_sec,
  );
  const buildBlock =
    farm.level < k.level
      ? `Cần cấp ${k.level}`
      : balance < k.price
        ? "Thiếu xu"
        : null;
  return (
    <div className="farm-section">
      {!k.built ? (
        <div className="shop-card kitchen-intro">
          <span className="shop-icon" aria-hidden>
            <Emo e="🍳" />
          </span>
          <div>
            <b>Xưởng chế biến</b>
            <small>
              Biến rau củ, trứng, sữa trong kho thành món ngon bán giá gấp
              rưỡi, xe tải còn đặt mua giá cao hơn. Có 2 lò, nâng lên 4 lò.
            </small>
          </div>
          <button
            type="button"
            className="btn btn-sm btn-primary"
            disabled={!!buildBlock || busy}
            onClick={onBuild}
          >
            {emo(buildBlock ?? `Chọn chỗ xây · ${k.price} 🪙`)}
          </button>
        </div>
      ) : (
        <>
          <div className="kitchen-head">
            <b>
              Lò nấu {jobs.size}/{k.cap}
            </b>
            <span className="kitchen-head-actions">
              <button
                type="button"
                className="btn btn-sm"
                onClick={onMove}
                title="Chọn chỗ khác trên bản đồ cho xưởng"
              >
                Đổi chỗ
              </button>
              {done > 0 ? (
                <button
                  type="button"
                  className="btn btn-sm btn-primary"
                  disabled={busy}
                  onClick={() => onCollect("all")}
                >
                  Lấy hết {done} món
                </button>
              ) : k.upgrade_price ? (
                <button
                  type="button"
                  className="btn btn-sm"
                  disabled={busy || balance < k.upgrade_price}
                  onClick={onUpgrade}
                >
                  +2 lò · {k.upgrade_price} <Emo e="🪙" />
                </button>
              ) : null}
            </span>
          </div>
          <ul className="kitchen-slots">
            {Array.from({ length: k.cap }, (_, slot) => {
              const j = jobs.get(slot);
              const r = j && byRecipe.get(j.recipe);
              if (!j || !r)
                return (
                  <li key={slot} className="kitchen-slot is-empty">
                    <span aria-hidden>
                      <Emo e="♨️" />
                    </span>
                    <small>Lò trống</small>
                  </li>
                );
              const left = new Date(j.ready_at).getTime() - now;
              const pct = Math.min(
                100,
                Math.max(0, 100 - (left / (r.time_sec * 1000)) * 100),
              );
              return left <= 0 ? (
                <li key={slot} className="kitchen-slot is-ready">
                  <button
                    type="button"
                    disabled={busy}
                    onClick={() => onCollect(slot)}
                    title={`Lấy ${r.name.toLowerCase()} vào kho`}
                  >
                    <span aria-hidden>
                      <Emo e={r.emoji} />
                    </span>
                    <small>Lấy</small>
                  </button>
                </li>
              ) : (
                <li key={slot} className="kitchen-slot" title={r.name}>
                  <span aria-hidden>
                    <Emo e={r.emoji} />
                  </span>
                  <span className="quest-bar">
                    <i style={{ width: `${pct}%` }} />
                  </span>
                  <small>{formatLeft(left)}</small>
                </li>
              );
            })}
          </ul>
        </>
      )}

      <h3 className="farm-h">Món chế biến</h3>
      <ul className="recipe-list">
        {sorted.map((r) => {
          const locked = farm.level < r.level;
          const short = r.inputs.some((i) => (stock[i.item] ?? 0) < i.qty);
          const block = locked
            ? `Cấp ${r.level}`
            : !k.built
              ? "Chưa xây"
              : short
                ? "Thiếu"
                : free <= 0
                  ? "Lò bận"
                  : null;
          return (
            <li
              key={r.id}
              className={`recipe-card${locked ? " is-locked" : ""}${!block ? " is-ready" : ""}`}
            >
              <span className="stock-emoji" aria-hidden>
                <Emo e={r.emoji} />
              </span>
              <div className="recipe-main">
                <b>{r.name}</b>
                <ul className="recipe-inputs">
                  {r.inputs.map((i) => {
                    const have = stock[i.item] ?? 0;
                    const it = items.get(i.item);
                    return (
                      <li
                        key={i.item}
                        className={have >= i.qty ? "is-ok" : ""}
                        title={it?.name}
                      >
                        <Emo e={it?.emoji ?? "📦"} />
                        <b>
                          {Math.min(have, i.qty)}/{i.qty}
                        </b>
                      </li>
                    );
                  })}
                </ul>
                <small>
                  <Emo e="⏱️" /> {formatGrow(r.time_sec)} · {r.price}{" "}
                  <Emo e="🪙" /> · +{r.xp} XP
                </small>
              </div>
              <button
                type="button"
                className="btn btn-sm btn-primary"
                disabled={!!block || busy}
                onClick={() => onCraft(r)}
              >
                {block ?? "Nấu"}
              </button>
            </li>
          );
        })}
      </ul>
    </div>
  );
}

/* =========================================================
   Đơn hàng (xe tải) & Kho
   ========================================================= */

function OrdersPanel({
  farm,
  now,
  busy,
  onDeliver,
  onDiscard,
}: {
  farm: FarmView;
  now: number;
  busy: boolean;
  onDeliver: (id: number) => void;
  onDiscard: (id: number) => void;
}) {
  const items = new Map((farm.items ?? []).map((i) => [i.id, i]));
  const stock = farm.storage?.stock ?? {};
  if (!farm.orders)
    return (
      <EmptyState icon="clock" title="Xe tải chưa tới">
        Đơn hàng sẽ có sau khi máy chủ cập nhật.
      </EmptyState>
    );
  return (
    <div className="farm-section">
      <p className="farm-foot">
        Gom đủ hàng trong kho rồi giao cho xe tải: lời hơn bán lẻ khoảng 35%.
      </p>
      <ul className="order-list">
        {farm.orders.map((o) =>
          o.id ? (
            <li
              key={o.slot}
              className={`order-card${o.can_deliver ? " is-ready" : ""}`}
            >
              <ul className="order-items">
                {o.items?.map((it) => {
                  const have = stock[it.item] ?? 0;
                  const info = items.get(it.item);
                  return (
                    <li
                      key={it.item}
                      className={have >= it.qty ? "is-ok" : ""}
                      title={info?.name}
                    >
                      <Emo e={info?.emoji ?? "📦"} />
                      <b>
                        {Math.min(have, it.qty)}/{it.qty}
                      </b>
                    </li>
                  );
                })}
              </ul>
              <div className="order-foot">
                <span className="order-reward">
                  +{o.xu} <Emo e="🪙" /> · +{o.xp} XP
                </span>
                <button
                  type="button"
                  className="btn btn-sm"
                  disabled={busy}
                  onClick={() => onDiscard(o.id!)}
                  title="Huỷ đơn"
                >
                  <Emo e="🗑️" />
                </button>
                <button
                  type="button"
                  className="btn btn-sm btn-primary"
                  disabled={busy || !o.can_deliver}
                  onClick={() => onDeliver(o.id!)}
                >
                  Giao
                </button>
              </div>
            </li>
          ) : (
            <li key={o.slot} className="order-card is-empty">
              <span aria-hidden><Emo e="🚚" /><Emo e="💨" /></span>
              <small>
                Đơn mới tới sau{" "}
                {o.next_at
                  ? formatLeft(new Date(o.next_at).getTime() - now)
                  : "ít phút"}
              </small>
            </li>
          ),
        )}
      </ul>
    </div>
  );
}

function StoragePanel({
  farm,
  busy,
  onSell,
  onBuy,
  onUpgrade,
}: {
  farm: FarmView;
  busy: boolean;
  onSell: (item: string | "all", qty?: number) => void;
  onBuy: (item: string, qty: number) => void;
  onUpgrade: () => void;
}) {
  const st = farm.storage;
  if (!st)
    return (
      <EmptyState icon="clock" title="Kho chưa mở">
        Kho sẽ có sau khi máy chủ cập nhật.
      </EmptyState>
    );
  const rows = (farm.items ?? []).filter((i) => (st.stock[i.id] ?? 0) > 0);
  const total = rows.reduce((n, i) => n + (st.stock[i.id] ?? 0) * i.price, 0);
  const pct = Math.min(100, (st.used / Math.max(1, st.cap)) * 100);
  return (
    <div className="farm-section">
      <div className={`storage-cap${pct >= 90 ? " is-full" : ""}`}>
        <div>
          <b>
            Kho {st.used}/{st.cap}
          </b>
          <span className="quest-bar">
            <i style={{ width: `${pct}%` }} />
          </span>
        </div>
        {st.next_upgrade && (
          <button
            type="button"
            className="btn btn-sm"
            disabled={
              busy || (farm.wallet?.balance ?? 0) < st.next_upgrade.price
            }
            onClick={onUpgrade}
          >
            Nới kho · {st.next_upgrade.price} <Emo e="🪙" />
          </button>
        )}
      </div>
      {rows.length === 0 ? (
        <EmptyState icon="clock" title="Kho trống">
          Thu hoạch ruộng và thu trứng, sữa… sẽ cất vào đây. Cần thức ăn ngay
          thì mua ở chợ bên dưới.
        </EmptyState>
      ) : (
        <>
          <ul className="stock-list">
            {rows.map((i) => {
              const n = st.stock[i.id] ?? 0;
              return (
                <li key={i.id}>
                  <span className="stock-emoji" aria-hidden>
                    <Emo e={i.emoji} />
                  </span>
                  <span className="stock-main">
                    <b>{i.name}</b>
                    <small>
                      ×{n} · {i.price} <Emo e="🪙" />/cái
                    </small>
                  </span>
                  <button
                    type="button"
                    className="btn btn-sm"
                    disabled={busy}
                    onClick={() => onSell(i.id, 1)}
                  >
                    Bán 1
                  </button>
                  <button
                    type="button"
                    className="btn btn-sm btn-primary"
                    disabled={busy}
                    onClick={() => onSell(i.id, n)}
                  >
                    Bán hết
                  </button>
                </li>
              );
            })}
          </ul>
          <button
            type="button"
            className="btn btn-primary btn-block"
            disabled={busy}
            onClick={() => onSell("all")}
          >
            Bán tất cả · {total.toLocaleString("vi-VN")} <Emo e="🪙" />
          </button>
        </>
      )}
      <MarketList farm={farm} busy={busy} onBuy={onBuy} />
    </div>
  );
}

/** Chợ: mua nông sản làm thức ăn cho vật nuôi (đắt hơn giá bán), chỉ loại đã mở theo cấp. */
function MarketList({
  farm,
  busy,
  onBuy,
}: {
  farm: FarmView;
  busy: boolean;
  onBuy: (item: string, qty: number) => void;
}) {
  const balance = farm.wallet?.balance ?? 0;
  const feeds = new Map((farm.animal_kinds ?? []).map((k) => [k.feed, k.name]));
  const goods = (farm.items ?? []).filter(
    (i) => (i.buy_price ?? 0) > 0 && i.level <= farm.level,
  );
  if (goods.length === 0) return null;
  // món vật nuôi đang cần lên đầu
  goods.sort(
    (a, b) =>
      Number(feeds.has(b.id)) - Number(feeds.has(a.id)) || a.level - b.level,
  );
  return (
    <>
      <h3 className="farm-h"><Emo e="🛒" /> Chợ: mua thức ăn</h3>
      <p className="farm-foot">
        Thiếu thức ăn cho vật nuôi thì mua ở đây (đắt hơn tự trồng).
      </p>
      <ul className="stock-list">
        {goods.map((i) => (
          <li key={i.id}>
            <span className="stock-emoji" aria-hidden>
              <Emo e={i.emoji} />
            </span>
            <span className="stock-main">
              <b>{i.name}</b>
              <small>
                {i.buy_price} <Emo e="🪙" />/cái
                {feeds.has(i.id) ? ` · ${feeds.get(i.id)} ăn` : ""}
              </small>
            </span>
            <button
              type="button"
              className="btn btn-sm"
              disabled={busy || balance < (i.buy_price ?? 0)}
              onClick={() => onBuy(i.id, 1)}
            >
              Mua 1
            </button>
            <button
              type="button"
              className="btn btn-sm btn-primary"
              disabled={busy || balance < (i.buy_price ?? 0) * 5}
              onClick={() => onBuy(i.id, 5)}
            >
              Mua 5
            </button>
          </li>
        ))}
      </ul>
    </>
  );
}

/* =========================================================
   Nhật ký & xếp hạng
   ========================================================= */

function logText(l: FarmLog, crops: Crop[], items: Item[] = []): string {
  const who = displayName(l.actor);
  const crop = crops.find((c) => c.id === l.crop);
  const name = crop ? crop.name.toLowerCase() : "cây";
  switch (l.action) {
    case "water":
      return `💧 ${who} tưới ${name} giúp bạn`;
    case "bug":
      return `🐛 ${who} bắt sâu cho ${name} của bạn`;
    case "steal":
      return `🫣 ${who} trộm 1 ${crop?.emoji ?? ""} ${name} của bạn`;
    case "dog":
      return `🐕 Chó đuổi ${who} chạy mất dép, bạn được ${l.xu ?? 10} xu`;
    case "propose":
      return `💌 ${who} ngỏ lời kết đôi với bạn`;
    case "couple":
      return `💞 ${who} và bạn đã thành một đôi`;
    case "breakup":
      return `💔 ${who} đã chia tay bạn`;
    case "gift": {
      const it = items.find((i) => i.id === l.crop);
      return `🎁 ${who} tặng bạn ${l.xu ?? 1} ${it?.emoji ?? "📦"} ${it?.name.toLowerCase() ?? ""}`.trim();
    }
  }
}

function ago(at: string, now: number) {
  const s = Math.max(0, Math.round((now - new Date(at).getTime()) / 1000));
  if (s < 60) return "vừa xong";
  if (s < 3600) return `${Math.floor(s / 60)} phút trước`;
  if (s < 86400) return `${Math.floor(s / 3600)} giờ trước`;
  return `${Math.floor(s / 86400)} ngày trước`;
}

function LogPanel({
  farm,
  now,
  onVisit,
}: {
  farm: FarmView;
  now: number;
  onVisit: (id: number) => void;
}) {
  const log = farm.log ?? [];
  if (log.length === 0)
    return (
      <EmptyState icon="clock" title="Vườn yên ắng">
        Khi hàng xóm tưới giúp, bắt sâu hay trộm rau của bạn, nhật ký sẽ ghi lại
        ở đây.
      </EmptyState>
    );
  return (
    <ul className="farm-log">
      {log.map((l, i) => (
        <li key={i} className={`is-${l.action}`}>
          <button
            type="button"
            className="farm-log-actor"
            onClick={() => onVisit(l.actor.id)}
            title="Ghé vườn người này"
          >
            <Avatar user={l.actor} size={28} />
          </button>
          <span>
            {emo(logText(l, farm.crops, farm.items))}
            <small>{ago(l.at, now)}</small>
          </span>
        </li>
      ))}
    </ul>
  );
}

function LeaderPanel({
  meId,
  onVisit,
}: {
  meId: number;
  onVisit: (id: number) => void;
}) {
  const { api } = useChat();
  const [rows, setRows] = useState<LeaderRow[] | null>(null);
  useEffect(() => {
    api
      .farmLeaderboard()
      .then(setRows)
      .catch(() => setRows([]));
  }, [api]);
  if (!rows) return <p className="muted">Đang tải…</p>;
  if (rows.length === 0)
    return <EmptyState icon="users" title="Chưa ai làm vườn" />;
  return (
    <ol className="farm-top">
      {rows.map((r, i) => (
        <li key={r.user.id} className={r.user.id === meId ? "is-me" : ""}>
          <span className="farm-rank">
            {i < 3 ? <Emo e={["🥇", "🥈", "🥉"][i]} /> : i + 1}
          </span>
          <button
            type="button"
            className="farm-top-user"
            onClick={() => onVisit(r.user.id)}
          >
            <Avatar user={r.user} size={30} />
            <b>{displayName(r.user)}</b>
          </button>
          <span className="farm-top-lv">Cấp {r.level}</span>
          <span className="farm-top-xp">{r.xp} XP</span>
        </li>
      ))}
    </ol>
  );
}

/* =========================================================
   Thẻ "Nông trại" ở thanh bên: số vụ chín / thời gian tới vụ chín
   ========================================================= */

export function FarmSidebarCard() {
  const { api } = useChat();
  const open = useFarmOpen();
  const [farm, setFarm] = useState<FarmView | null>(null);
  const [missing, setMissing] = useState(false);
  const now = useServerNow(farm?.now);
  const load = useCallback(
    () =>
      api
        .getFarm()
        .then(setFarm)
        .catch((err) => {
          if (err instanceof ApiError && err.status === 404) setMissing(true);
        }),
    [api],
  );
  useEffect(() => {
    void load();
    const t = window.setInterval(() => void load(), 120_000);
    return () => window.clearInterval(t);
  }, [load]);
  // đóng màn hình nông trại xong thì cập nhật lại số liệu
  useEffect(() => {
    if (!open) void load();
  }, [open, load]);
  useEffect(() => farmEvents.on(() => void load()), [load]);

  const info = useMemo(() => {
    if (!farm) return null;
    const lives = farm.plots.map((p) => liveState(p, now));
    const ripe = lives.filter((l) => l.state === "ripe").length;
    const withered = lives.filter((l) => l.state === "withered").length;
    const next = lives
      .filter((l) => l.state === "growing")
      .sort((a, b) => a.left - b.left)[0];
    const empty = lives.filter((l) => l.state === "empty").length;
    const quests = questsReady(farm);
    return { ripe, withered, next, empty, quests };
  }, [farm, now]);

  if (missing) return null;
  const sub = !info
    ? "Trồng rau, kiếm xu"
    : info.ripe > 0
      ? `${info.ripe} vụ đã chín, hái ngay!`
      : info.quests > 0
        ? `${info.quests} phần thưởng chờ nhận`
        : info.next
          ? `Vụ tới chín sau ${formatLeft(info.next.left)}`
          : info.empty > 0
            ? `${info.empty} ô đất đang trống`
            : "Trồng rau, kiếm xu";
  const hot = !!info && (info.ripe > 0 || info.quests > 0);
  return (
    <button
      type="button"
      className={`farm-card${hot ? " is-hot" : ""}`}
      onClick={() => farmScreen.open()}
    >
      <span className="farm-card-icon" aria-hidden>
        <Emo e="🌾" />
      </span>
      <span className="farm-card-text">
        <b>Nông trại</b>
        <small>{sub}</small>
      </span>
      {info && info.ripe > 0 && (
        <span className="farm-card-badge">{info.ripe}</span>
      )}
    </button>
  );
}
