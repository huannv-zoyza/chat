import {
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
  type CSSProperties,
  type ReactNode,
} from "react";
import { createPortal } from "react-dom";
import { ApiError, type Wallet } from "../lib/api";
import { displayName } from "../lib/format";
import {
  farmEvents,
  farmMarketEvents,
  farmXmasEvents,
  effAnimalSec,
  effGrowSec,
  effSellPrice,
  effSellTotal,
  farmScreen,
  formatGrow,
  hasNextLevel,
  perkGains,
  formatLeft,
  orchardUnlockLevel,
  ORCHARD_SPOTS,
  liveState,
  treeLive,
  useFarmOpen,
  type Animal,
  type Crop,
  type FarmLog,
  type FarmView,
  type LeaderRow,
  type Neighbor,
  type Plot,
  type PlotState,
  type Perks,
  type Quest,
  type Item,
  type Recipe,
  type FruitTree,
  type TreeView,
  type Spot,
  type MarketListing,
  type XmasReward,
  type XmasView,
  spotProblem,
} from "../lib/farm";
import {
  autoPending,
  autoSummary,
  emptyPlots,
  waterablePlots,
  buggyPlots,
  loadAuto,
  saveAuto,
  type FarmAutoSettings,
} from "../lib/farmAuto";
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
  | "orchard"
  | "shop"
  | "market"
  | "xmas"
  | "neighbors"
  | "log"
  | "top";
const TABS: { id: Tab; label: string; icon: string }[] = [
  { id: "quests", label: "Nhiệm vụ", icon: "📋" },
  { id: "orders", label: "Đơn hàng", icon: "🚚" },
  { id: "storage", label: "Kho", icon: "🏚️" },
  { id: "kitchen", label: "Xưởng", icon: "🍳" },
  { id: "orchard", label: "Vườn & Ép", icon: "🍊" },
  { id: "shop", label: "Cửa hàng", icon: "🛒" },
  { id: "market", label: "Chợ đen", icon: "🕶️" },
  { id: "xmas", label: "Noel", icon: "🎄" },
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
  const [sockOpen, setSockOpen] = useState(false);
  const [treeOpen, setTreeOpen] = useState(false);
  const [santaOpen, setSantaOpen] = useState(false);
  const eveGift = () =>
    void act(-1, () => api.farmXmasEveGift(), (f) =>
      showReveal({
        box: "🛷",
        title: "Quà đêm Noel của Ông già Noel 🎅",
        rare: true,
        prizes: [
          { emoji: "🍬", label: `${f.gained?.candy ?? 0} Kẹo Noel` },
          { emoji: "🪙", label: `${f.gained?.xu ?? 0} xu` },
          { emoji: "🔔", label: "Chuông Đêm Thánh", sub: "Đồ trang trí chỉ có đêm Noel, đã treo trong vườn" },
        ],
      }),
    );
  // giới thiệu sự kiện Giáng sinh: tự mở lần đầu vào nông trại trong sự kiện (nhớ theo máy), mở lại ở tab Noel
  const [introOpen, setIntroOpen] = useState(false);
  const xmasIntroKey = mine?.xmas?.on ? `farm.xmas.intro.${mine.xmas.from}` : null;
  useEffect(() => {
    if (!xmasIntroKey) return;
    let seen = false;
    try {
      seen = localStorage.getItem(xmasIntroKey) === "1";
    } catch {
      // không đọc được bộ nhớ: cứ hiện, đóng là xong
    }
    if (!seen) setIntroOpen(true);
  }, [xmasIntroKey]);
  const closeIntro = () => {
    setIntroOpen(false);
    try {
      if (xmasIntroKey) localStorage.setItem(xmasIntroKey, "1");
    } catch {
      // bỏ qua: lần sau hiện lại
    }
  };
  // popup mở quà (hộp bí ẩn, quà Ông già Noel, tất Noel); act không trả FarmView nên dựng popup trong okText
  const [reveal, setReveal] = useState<Reveal | null>(null);
  const showReveal = (r: Reveal | null) => {
    if (r) setReveal(r);
    return undefined; // không hiện toast: popup đã báo
  };
  const openSocks = () => void act(-1, () => api.farmXmasOpenSocks(), (f) => showReveal(socksReveal(f)));
  const claimDrop = (id: number) =>
    void act(-1, () => api.farmXmasClaimDrop(id), (f) => showReveal(dropReveal(f)));
  const throwSnow = (id: number) =>
    void act(-1, () => api.farmXmasSnowball(id), (f) =>
      f.gained?.snow === "miss"
        ? "Trượt mất rồi! ⛄"
        : f.gained?.snow === "revenge"
          ? "Ném trả trúng phóc! ⛄ +thưởng trả đũa"
          : "Trúng rồi! ⛄",
    );
  // chỗ trồng cây đang chọn (bấm chỗ trống trên bản đồ) và cây đang hỏi chặt
  const [treeSpot, setTreeSpot] = useState<number | null>(null);
  const [chop, setChop] = useState<TreeView | null>(null);
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
        // tin Noel báo riêng đã có khung XmasNotice (kèm nút ném trả), không hiện trùng
        if (l.action === "snowball" || l.action === "drop_claimed" || l.action === "sock_opened") return;
        showToast(
          logText(l, mine?.crops ?? [], mine?.items),
          l.action === "steal" || l.action === "breakup" ? "bad" : "good",
        );
      }),
    [loadMine, showToast, mine?.crops],
  );

  // chợ đen chung toàn server vừa đổi (ai đó treo / mua / gỡ hàng)
  useEffect(() => farmMarketEvents.on(() => void loadMine()), [loadMine]);
  // tin Giáng sinh toàn server (quà vừa rơi / bị nhặt, cây chung lên mốc): tải lại để thấy quà mới
  useEffect(() => farmXmasEvents.on(() => void loadMine()), [loadMine]);

  /** Gọi một hành động, áp kết quả, bay số "+xu" lên ô vừa làm. */
  const act = async (
    plot: number | null,
    fn: () => Promise<FarmView>,
    okText?: string | ((f: FarmView) => string | undefined),
    /** có: lỗi giao cho người gọi (vòng tự động) thay vì hiện ở thanh lỗi */
    onError?: (msg: string) => void,
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
        if (g.candy)
          pop(at, `${g.candy > 0 ? "+" : ""}${g.candy} 🍬`, g.candy > 0 ? "xu" : "bad", 260);
        if (g.xu > 0) playCoinSound();
      }
      const okMsg = okText
        ? typeof okText === "function"
          ? okText(f)
          : okText
        : undefined;
      // showToast thay toast cũ, nên khi lên cấp thì nối kết quả hành động vào cùng một toast
      const withOk = (t: string) => (okMsg ? `${t} ${okMsg}` : t);
      if (g?.love_up) {
        const c = f.couple ?? mine?.couple;
        showToast(
          withOk(
            `💞 Thân mật lên cấp ${g.love_up}${c ? `: ${c.level_name}` : ""}! Cả hai cùng nhận ${30 * g.love_up} xu.`,
          ),
          "level",
        );
      } else if (g?.level_up)
        showToast(
          withOk(
            `🎉 Lên cấp ${g.level_up}! Mở khoá cây mới trong cửa hàng hạt.${
              perkGains(mine?.perks, f.perks).length
                ? ` ${perkGains(mine?.perks, f.perks).join(", ")}.`
                : ""
            }`,
          ),
          "level",
        );
      else if (okMsg) showToast(okMsg, g?.hungry ? "bad" : "good");
      return true;
    } catch (err) {
      if (onError) onError(errMsg(err));
      else setError(errMsg(err));
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

  // ---- tự động: "Làm hết", "Gieo tất cả" và vòng 30 giây ----
  const meId = state.me.id;
  const [auto, setAuto] = useState<FarmAutoSettings>(() => loadAuto(meId));
  const [autoOpen, setAutoOpen] = useState(false);
  const pausedUntil = useRef(0);
  const patchAuto = (p: Partial<FarmAutoSettings>) =>
    setAuto((a) => {
      const n = { ...a, ...p };
      saveAuto(meId, n);
      return n;
    });
  const autoOpts = () => ({
    crop: auto.crop,
    kitchen_recipe: auto.kitchen_recipe,
    juicer_recipe: auto.juicer_recipe,
    buy_feed: auto.buy_feed,
  });
  const runAuto = (prefix: string, onError?: (m: string) => void) =>
    act(
      null,
      () => api.farmAuto(autoOpts()),
      (f) => (f.gained?.auto ? autoSummary(f.gained.auto, prefix) : undefined),
      onError,
    );
  const bugAll = () =>
    void act(
      null,
      () => api.farmBugAll(),
      (f) => `Đã bắt ${f.gained?.bugs ?? 0} con sâu 🐛`,
    );
  const waterAll = () =>
    void act(
      null,
      () => api.farmWaterAll(),
      (f) => `Đã tưới ${f.gained?.watered ?? 0} ô 💧`,
    );
  const plantAll = () => {
    if (!auto.crop) {
      setTab("shop");
      return showToast("Chọn loại cây để gieo tất cả");
    }
    const crop = auto.crop;
    void act(
      null,
      () => api.farmPlantAll(crop),
      (f) => `🌱 Đã gieo ${f.gained?.planted ?? 0} ô`,
    );
  };
  // tick mới nhất cho vòng lặp (tránh closure cũ), không bao giờ chồng hai lượt
  const autoTick = useRef<() => void>(() => {});
  autoTick.current = () => {
    if (busy || visit || !mine || !auto.on) return;
    if (Date.now() < pausedUntil.current) return;
    if (!autoPending(mine, now, auto)) return;
    void runAuto("🤖", (m) => {
      pausedUntil.current = Date.now() + 5 * 60_000;
      if (m !== "Chưa có việc gì để làm.") showToast(m, "bad");
    });
  };
  useEffect(() => {
    if (!auto.on || visit) return;
    const t = window.setInterval(() => autoTick.current(), 30_000);
    return () => window.clearInterval(t);
  }, [auto.on, visit]);

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
      (j) =>
        (j.building ?? "kitchen") === "kitchen" &&
        new Date(j.ready_at).getTime() <= now,
    ).length;
    setTab("kitchen");
    if (done > 0) return void act(-1, () => api.farmCraftCollect("all", "kitchen"));
    if (k && !k.built && view.level >= k.level)
      return startPlacing("kitchen", "build");
    if (k && !k.built)
      return showToast(
        `Lên cấp ${k.level} để xây xưởng chế biến: biến rau củ, trứng, sữa thành món giá cao.`,
      );
    scrollToPanel();
  };

  /** "Đã hái 3 🍊, 3 🍎" từ kết quả hái cây. */
  const harvestText = (f: FarmView) => {
    const catalog = f.items ?? view?.items ?? [];
    const parts = Object.entries(f.gained?.items ?? {}).map(
      ([id, n]) => `${n} ${catalog.find((x) => x.id === id)?.emoji ?? "📦"}`,
    );
    return parts.length ? `Đã hái ${parts.join(", ")}` : undefined;
  };
  /** Bấm cây: có quả thì hái, chưa thì cho biết còn bao lâu. */
  const onTree = (t: TreeView) => {
    if (!view?.me || busy) return;
    const k = view.tree_kinds?.find((x) => x.id === t.tree);
    const live = treeLive(t, now);
    if (live.ready)
      return void act(-1, () => api.farmHarvestTree(t.spot), harvestText);
    showToast(
      `${k?.name ?? "Cây"} còn ${formatLeft(live.left)} nữa mới có quả.`,
    );
  };
  /** Bấm chỗ trống trên bản đồ: mở cửa hàng cây giống ở tab Vườn & Ép với chỗ đó. */
  const onOrchardSpot = (spot: number) => {
    if (!view?.me || busy) return;
    setTreeSpot(spot);
    setTab("orchard");
    scrollToPanel();
  };
  const onOrchardHarvest = () => {
    if (!view?.me || busy) return;
    void act(-1, () => api.farmHarvestTree("all"), harvestText);
  };
  /** Bấm máy ép trên bản đồ: có món xong thì lấy hết, chưa xây thì xây (đủ cấp), còn lại mở tab Vườn & Ép. */
  const onJuicer = () => {
    if (!view?.me || busy) return;
    const j = view.buildings?.find((b) => b.id === "juicer");
    const done = jobsReady(view, now, "juicer");
    setTab("orchard");
    if (done > 0)
      return void act(-1, () => api.farmCraftCollect("all", "juicer"));
    if (j && !j.built && view.level >= j.level)
      return startPlacing("juicer", "build");
    if (j && !j.built)
      return showToast(
        `Lên cấp ${j.level} để xây máy ép nước: biến trái cây thành nước ép giá cao.`,
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
                    <>
                    {ripeCount > 0 ? (
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
                    )}
                    {autoPending(view, now, auto) && (
                      <button
                        type="button"
                        className="farm-auto-btn"
                        disabled={busy}
                        onClick={() => void runAuto("⚡")}
                      >
                        <Emo e="⚡" /> Làm hết
                      </button>
                    )}
                    {emptyPlots(view, now) > 0 && (
                      <button
                        type="button"
                        className="farm-auto-btn"
                        disabled={busy}
                        onClick={plantAll}
                      >
                        <Emo e="🌱" /> Gieo tất cả
                      </button>
                    )}
                    {buggyPlots(view) > 0 && (
                      <button
                        type="button"
                        className="farm-auto-btn"
                        disabled={busy}
                        onClick={bugAll}
                      >
                        <Emo e="🐛" /> Bắt sâu tất cả
                      </button>
                    )}
                    {waterablePlots(view, now) > 0 && (
                      <button
                        type="button"
                        className="farm-auto-btn"
                        disabled={busy}
                        onClick={waterAll}
                      >
                        <Emo e="💧" /> Tưới tất cả
                      </button>
                    )}
                    <div className="farm-auto">
                      <button
                        type="button"
                        className={`farm-auto-btn${auto.on ? " is-on" : ""}`}
                        aria-pressed={auto.on}
                        onClick={() => patchAuto({ on: !auto.on })}
                      >
                        <Emo e="🤖" /> Tự động
                      </button>
                      <button
                        type="button"
                        className="farm-auto-btn farm-auto-gear"
                        aria-label="Cài đặt tự động"
                        aria-expanded={autoOpen}
                        onClick={() => setAutoOpen((o) => !o)}
                      >
                        <Emo e="⚙️" />
                      </button>
                      {autoOpen && (
                        <AutoSettings
                          view={view}
                          value={auto}
                          onChange={patchAuto}
                          onClose={() => setAutoOpen(false)}
                        />
                      )}
                    </div>
                    </>
                  ) : (
                    <span className="farm-tip is-visit">
                      <Emo e="🫣" />
                      {view.xmas_snow ? (
                        // mùa Noel không trộm được: gợi ý trò Noel thay vì lượt trộm
                        <>Mùa Noel an lành: ném cầu tuyết, treo tất · ô đang lớn: tưới giúp</>
                      ) : (
                        <>
                          {view.steals_left !== undefined && (
                            <>
                              Còn <b>{view.steals_left}</b> lượt trộm ·{" "}
                            </>
                          )}
                          ô chín: trộm · ô đang lớn: tưới giúp
                        </>
                      )}
                    </span>
                  )}
                  {!view.me && view.xmas_snow && (
                    <button
                      type="button"
                      className="btn btn-sm"
                      disabled={busy || view.xmas_snow !== "can"}
                      title={
                        view.xmas_snow === "cooldown"
                          ? "Vừa ném vườn này, 30 phút nữa ném tiếp"
                          : view.xmas_snow === "none_left"
                            ? "Hôm nay hết cầu tuyết rồi"
                            : "Trúng được kẹo, ném trả người vừa ném mình được thêm"
                      }
                      onClick={() => throwSnow(view.user.id)}
                    >
                      <Emo e="⛄" /> Ném cầu tuyết
                    </button>
                  )}
                  {view.xmas_drop && (
                    <button
                      type="button"
                      className="btn btn-sm btn-primary xmas-drop-btn"
                      disabled={busy}
                      onClick={() => claimDrop(view.xmas_drop!.id)}
                    >
                      <Emo e="🎁" /> Nhặt quà Ông già Noel!
                    </button>
                  )}
                  {!view.me && view.xmas_sock && (
                    <button
                      type="button"
                      className="btn btn-sm"
                      disabled={busy || view.xmas_sock !== "can"}
                      title={
                        view.xmas_sock === "hung"
                          ? "Hôm nay bạn đã treo tất ở vườn này"
                          : view.xmas_sock === "none_left"
                            ? "Hôm nay bạn đã treo đủ tất"
                            : "Bỏ quà vào tất, mai chủ vườn mới mở"
                      }
                      onClick={() => setSockOpen(true)}
                    >
                      <Emo e="🧦" /> {view.xmas_sock === "hung" ? "Đã treo tất" : "Treo tất Noel"}
                    </button>
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
                  onTend={(id) =>
                    void act(
                      -1,
                      () => api.farmTend(id),
                      (f) => tendToast(f, view),
                    )
                  }
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
                  onXmasDrop={claimDrop}
                  onXmasOpenSocks={openSocks}
                  onXmasEveGift={eveGift}
                  onXmasDailyGift={() =>
                    void act(-1, () => api.farmXmasDailyGift(), (f) =>
                      showReveal(
                        f.gained?.candy
                          ? { box: "🎁", title: "Quà dưới gốc cây thông", prizes: [{ emoji: "🍬", label: `${f.gained.candy} Kẹo Noel`, sub: "Mai quay lại mở tiếp nhé!" }] }
                          : null,
                      ),
                    )
                  }
                  onKitchen={onKitchen}
                  onJuicer={onJuicer}
                  onTree={onTree}
                  onOrchardSpot={onOrchardSpot}
                  onOrchardHarvest={onOrchardHarvest}
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
              {TABS.filter((t) => t.id !== "xmas" || mine?.xmas).map((t) => (
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
                  {t.id === "xmas" && mine && xmasReady(mine) > 0 && (
                    <i className="farm-tab-count">{xmasReady(mine)}</i>
                  )}
                  {t.id === "orchard" && mine && orchardReady(mine, now) > 0 && (
                    <i className="farm-tab-count">{orchardReady(mine, now)}</i>
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
                  building="kitchen"
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
                  onFill={(r) =>
                    void act(
                      -1,
                      () => api.farmCraftFill(r.id),
                      `Đã làm đầy lò ${r.emoji}`,
                    )
                  }
                  onCollect={(slot) =>
                    void act(-1, () => api.farmCraftCollect(slot, "kitchen"))
                  }
                />
              )}
              {tab === "orchard" && mine && (
                <OrchardPanel
                  farm={mine}
                  now={now}
                  busy={busy}
                  spot={treeSpot}
                  onPick={setTreeSpot}
                  onPlant={(spot, k) =>
                    void act(
                      -1,
                      () => api.farmPlantTree(spot, k.id),
                      `Đã trồng ${k.name.toLowerCase()} ${k.emoji}`,
                    ).then((ok) => {
                      if (ok) setTreeSpot(null);
                    })
                  }
                  onHarvestAll={onOrchardHarvest}
                  onChop={setChop}
                  juicer={
                    <KitchenPanel
                      building="juicer"
                      farm={mine}
                      now={now}
                      busy={busy}
                      onBuild={() => startPlacing("juicer", "build")}
                      onMove={() => startPlacing("juicer", "move")}
                      onUpgrade={() =>
                        void act(
                          -1,
                          () => api.farmUpgradeBuilding("juicer"),
                          "Máy ép có thêm lò 🔥",
                        )
                      }
                      onCraft={(r) =>
                        void act(
                          -1,
                          () => api.farmCraft(r.id),
                          `Đang ép ${r.name.toLowerCase()} ${r.emoji}`,
                        )
                      }
                      onFill={(r) =>
                        void act(
                          -1,
                          () => api.farmCraftFill(r.id),
                          `Đã làm đầy lò ${r.emoji}`,
                        )
                      }
                      onCollect={(slot) =>
                        void act(-1, () => api.farmCraftCollect(slot, "juicer"))
                      }
                    />
                  }
                />
              )}
              {tab === "shop" && mine && (
                <ShopPanel
                  farm={mine}
                  now={now}
                  busy={busy}
                  onBuild={(id) =>
                    id === "kitchen" || id === "juicer"
                      ? startPlacing(id, "build")
                      : void act(-1, () => api.farmBuild(id), "Đã xây xong 🔨")
                  }
                  onUpgradeBuilding={(id) =>
                    void act(
                      -1,
                      () => api.farmUpgradeBuilding(id),
                      id === "kitchen"
                        ? "Xưởng có thêm lò 🔥"
                        : id === "juicer"
                          ? "Máy ép có thêm lò 🔥"
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
              {tab === "market" && mine && (
                <BlackMarketPanel
                  farm={mine}
                  now={now}
                  busy={busy}
                  onList={(item, qty, price) =>
                    void act(
                      -1,
                      () => api.farmMarketList(item, qty, price),
                      "Đã treo hàng lên chợ đen 🕶️",
                    )
                  }
                  onBuy={(l) =>
                    void act(
                      -1,
                      () => api.farmMarketBuy(l.id),
                      "Mua được hàng chợ đen, đã cất vào kho 📦",
                    )
                  }
                  onCancel={(l) =>
                    void act(
                      -1,
                      () => api.farmMarketCancel(l.id),
                      "Đã gỡ hàng, đồ về lại kho.",
                    )
                  }
                  onVisit={goVisit}
                />
              )}
              {tab === "xmas" && mine?.xmas && (
                <XmasPanel
                  farm={mine}
                  now={now}
                  busy={busy}
                  onDeliver={(slot) =>
                    void act(-1, () => api.farmXmasDeliver(slot), "Ông già Noel đã nhận hàng 🎅")
                  }
                  onBuy={(r) =>
                    void act(-1, () => api.farmXmasBuy(r.id), `Đã đổi ${r.name} 🎁`)
                  }
                  onClaimDrop={claimDrop}
                  onVisit={goVisit}
                  onBox={() =>
                    void act(-1, () => api.farmXmasBox(), (f) => showReveal(boxReveal(f)))
                  }
                  onTree={() => setTreeOpen(true)}
                  onRules={() => setIntroOpen(true)}
                  onEveGift={eveGift}
                  onSantaJoin={() =>
                    void act(-1, () => api.farmXmasSantaJoin(), "Đã đăng ký Ông già Noel bí mật 🎅 Chờ bốc thăm nhé!")
                  }
                  onSantaSend={() => setSantaOpen(true)}
                  onSantaOpen={() =>
                    void act(-1, () => api.farmXmasSantaOpen(), (f) => {
                      const r = socksReveal(f);
                      return showReveal(
                        r && {
                          ...r,
                          box: "🎅",
                          title: "Quà Ông già Noel bí mật",
                          rare: true,
                          prizes: r.prizes.map((p) => (p.emoji === "🍬" ? { ...p, sub: "thưởng mở quà" } : p)),
                        },
                      );
                    })
                  }
                  onMilestone={(n) =>
                    void act(-1, () => api.farmXmasMilestone(n), (f) => {
                      const r = boxReveal(f);
                      return showReveal(r && { ...r, title: `Mốc ${f.xmas?.milestones[n - 1]?.need ?? ""} kẹo Noel` });
                    })
                  }
                  onTreeClaim={(goal) =>
                    void act(-1, () => api.farmXmasTreeClaim(goal), `Nhận thưởng mốc ${goal} cây thông chung 🎄`)
                  }
                  onOpenSocks={openSocks}
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
      {sockOpen && mine && visit && (
        <GiftDialog
          farm={mine}
          busy={busy}
          title={`Treo tất Noel ở vườn ${displayName(visit.user)}`}
          maxQty={mine.xmas?.sock_max_qty ?? 5}
          note={
            <>
              Mai chủ vườn mới mở tất và biết bạn tặng · +2 <Emo e="🍬" /> · còn{" "}
              {mine.xmas?.socks_left ?? 0} tất hôm nay
            </>
          }
          canSend={(mine.xmas?.socks_left ?? 0) > 0}
          sendLabel="Bỏ vào tất"
          onClose={() => setSockOpen(false)}
          onGift={async (item, qty) => {
            const ok = await act(
              -1,
              () => api.farmXmasSock(visit.user.id, item, qty),
              `Đã treo tất ở vườn ${displayName(visit.user)} 🧦`,
            );
            if (ok) setSockOpen(false);
          }}
        />
      )}
      {reveal && <XmasReveal reveal={reveal} onClose={() => setReveal(null)} />}
      {introOpen && mine?.xmas && (
        <XmasIntro
          x={mine.xmas}
          onClose={closeIntro}
          onGo={() => {
            closeIntro();
            openTab("xmas");
          }}
        />
      )}
      {santaOpen && mine?.xmas?.santa.target && (
        <GiftDialog
          farm={mine}
          busy={busy}
          title={`Quà bí mật cho ${displayName(mine.xmas.santa.target)} 🤫`}
          maxQty={mine.xmas.santa.max_qty}
          note={<>Chỉ gửi được một lần. Đêm Noel {displayName(mine.xmas.santa.target)} mới mở và biết là bạn · +5 <Emo e="🍬" /></>}
          canSend
          sendLabel="Gửi quà"
          onClose={() => setSantaOpen(false)}
          onGift={async (item, qty) => {
            const ok = await act(-1, () => api.farmXmasSantaSend(item, qty), "Đã gói quà bí mật 🎁 Giữ kín tới đêm Noel nhé!");
            if (ok) setSantaOpen(false);
          }}
        />
      )}
      {treeOpen && mine?.xmas && (
        <GiftDialog
          farm={mine}
          busy={busy}
          title="Góp vào cây thông chung 🎄"
          maxQty={50}
          note={
            <>
              Mỗi 60 điểm (giá bán) được 1 <Emo e="🍬" />, hôm nay còn nhận được {mine.xmas.tree_candy_left} kẹo · góp đủ{" "}
              {mine.xmas.tree_min_share} điểm để nhận thưởng mốc
            </>
          }
          canSend
          sendLabel="Góp"
          onClose={() => setTreeOpen(false)}
          onGift={async (item, qty) => {
            const ok = await act(-1, () => api.farmXmasTree(item, qty), "Cảm ơn bạn đã góp vào cây thông chung 🎄");
            if (ok) setTreeOpen(false);
          }}
        />
      )}
      {chop && mine && (
        <Dialog
          title="Chặt cây?"
          onClose={() => setChop(null)}
          className="farm-confirm"
        >
          <div className="farm-confirm-body">
            <span className="farm-confirm-icon" aria-hidden>
              <Emo e="🪓" />
            </span>
            <p>
              Chặt cây {mine.tree_kinds?.find((k) => k.id === chop.tree)?.name}?
              Không hoàn lại tiền cây giống.
            </p>
          </div>
          <div className="farm-confirm-actions">
            <button type="button" className="btn" onClick={() => setChop(null)}>
              Để sau
            </button>
            <button
              type="button"
              className="btn btn-danger"
              onClick={() => {
                const t = chop;
                setChop(null);
                void act(-1, () => api.farmRemoveTree(t.spot), "Đã chặt cây 🪓");
              }}
            >
              Chặt cây
            </button>
          </div>
        </Dialog>
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
  const [open, setOpen] = useState(false);
  const hasNext = hasNextLevel(view);
  const span = hasNext ? view.next_level_xp! - view.level_xp : 1;
  const pct = hasNext
    ? Math.min(100, ((view.xp - view.level_xp) / span) * 100)
    : 100;
  const rows: { label: (n: number) => string; get: (p: Perks) => number }[] = [
    { label: (n) => `🌱 Cây lớn nhanh hơn ${n}%`, get: (p) => p.grow_pct },
    { label: (n) => `🐓 Vật nuôi làm nhanh hơn ${n}%`, get: (p) => p.animal_pct },
    { label: (n) => `🧺 Thu hoạch thêm +${n}/vụ`, get: (p) => p.harvest_bonus },
    { label: (n) => `🪙 Bán giá cao hơn ${n}%`, get: (p) => p.sell_pct },
  ];
  const perks = view.perks;
  return (
    <div
      className="farm-level"
      title={hasNext ? `${view.xp}/${view.next_level_xp} XP` : "Cấp tối đa"}
    >
      <button
        type="button"
        className="farm-level-badge farm-level-badge-btn"
        aria-expanded={open}
        disabled={!perks}
        onClick={() => setOpen((o) => !o)}
      >
        Cấp {view.level}
      </button>
      <span className="farm-level-bar">
        <i style={{ width: `${pct}%` }} />
      </span>
      <span className="farm-level-xp">
        {hasNext ? `${view.xp}/${view.next_level_xp} XP` : "Tối đa"}
      </span>
      {open && perks && (
        <ul className="farm-perks">
          {rows.map((r) => {
            const cur = r.get(perks);
            const next = view.next_perks ? r.get(view.next_perks) : cur;
            return (
              <li key={r.label(0)}>
                <Emo e={r.label(cur).slice(0, 2)} />
                {r.label(cur).slice(2)}{" "}
                <span className="farm-perk-next">
                  {next > cur ? `→ ${next}` : "(tối đa)"}
                </span>
              </li>
            );
          })}
        </ul>
      )}
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
                <span className="seed-time"><Emo e="⏱" /> {formatGrow(effGrowSec(c.grow_sec, farm.perks))}</span>
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

/** Lời báo sau khi chăm nhanh một chuồng. */
function tendToast(f: FarmView, before: FarmView | null): string | undefined {
  const g = f.gained;
  if (!g) return undefined;
  const collected = Object.values(g.items ?? {}).reduce((a, b) => a + b, 0);
  const catalog = f.items ?? before?.items ?? [];
  const bought = Object.entries(g.bought ?? {})
    .map(
      ([id, n]) =>
        `${n}${catalog.find((x) => x.id === id)?.emoji ?? f.crops.find((c) => c.id === id)?.emoji ?? "📦"}`,
    )
    .join(" ");
  const fed = g.fed ?? 0;
  const parts: string[] = [];
  if (collected > 0) parts.push(`Đã thu ${collected} sản phẩm`);
  if (fed > 0) parts.push(`${collected > 0 ? "cho" : "Đã cho"} ${fed} con ăn`);
  let t = parts.join(", ");
  if (bought) t += `${t ? " " : "Đã "}(mua thêm ${bought})`;
  if (g.hungry) {
    const warn = `Còn ${g.hungry} con đói: không đủ xu hoặc chưa đủ cấp mua thức ăn.`;
    t = t ? `${t}. ${warn}` : warn;
  }
  return t || undefined;
}

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
const jobsReady = (
  f: FarmView,
  now: number,
  building: "kitchen" | "juicer" = "kitchen",
) =>
  (f.jobs ?? []).filter(
    (j) =>
      (j.building ?? "kitchen") === building &&
      new Date(j.ready_at).getTime() <= now,
  ).length;

/** Số việc chờ ở tab Vườn & Ép: cây có quả + lò máy ép đã xong. */
const orchardReady = (f: FarmView, now: number) =>
  (f.trees ?? []).filter((t) => treeLive(t, now).ready).length +
  jobsReady(f, now, "juicer");

const xmasReady = (f: FarmView) =>
  (f.xmas?.orders ?? []).filter((o) => o.can_deliver).length +
  (f.xmas?.socks_ready ? 1 : 0);

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
          <span className="love-request-text">
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
          <span className="love-request-text">
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

/** Tặng nông sản / sản phẩm trong kho cho người thương (mặc định), hoặc bỏ vào tất Noel (truyền
 *  title / maxQty / note / canSend / sendLabel). */
function GiftDialog({
  farm,
  busy,
  onClose,
  onGift,
  title,
  maxQty = 10,
  note,
  canSend,
  sendLabel = "Tặng",
}: {
  farm: FarmView;
  busy: boolean;
  onClose: () => void;
  onGift: (item: string, qty: number) => void;
  title?: string;
  maxQty?: number;
  note?: ReactNode;
  canSend?: boolean;
  sendLabel?: string;
}) {
  const stock = farm.storage?.stock ?? {};
  const items = (farm.items ?? []).filter((i) => (stock[i.id] ?? 0) > 0);
  const [pick, setPick] = useState<string | null>(items[0]?.id ?? null);
  const [qty, setQty] = useState(1);
  const it = items.find((i) => i.id === pick);
  const have = it ? (stock[it.id] ?? 0) : 0;
  const n = Math.min(qty, have, maxQty);
  const love = it ? Math.min(10, Math.max(1, Math.ceil((it.price * n) / 20))) : 0;
  const partner = farm.couple?.partner;
  const can = canSend ?? !!farm.couple?.gifts_left;
  return (
    <Dialog
      title={title ?? `Tặng quà cho ${partner ? displayName(partner) : "người thương"}`}
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
                {[1, 5, 10, 50].filter((q) => q <= maxQty).map((q) => (
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
            {note ?? (
              <>
                +{love} <Emo e="💗" /> thân mật · còn {farm.couple?.gifts_left ?? 0}{" "}
                lần tặng hôm nay
              </>
            )}
          </p>
          <div className="farm-confirm-actions">
            <button type="button" className="btn" onClick={onClose}>
              Để sau
            </button>
            <button
              type="button"
              className="btn btn-primary"
              disabled={busy || !it || n < 1 || !can}
              onClick={() => it && onGift(it.id, n)}
            >
              {sendLabel} {n} <Emo e={it?.emoji} />
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
                e={
                  b.id === "coop"
                    ? "🛖"
                    : b.id === "kitchen"
                      ? "🍳"
                      : b.id === "juicer"
                        ? "🧃"
                        : "🌿"
                }
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
                    : b.id === "juicer"
                      ? b.built
                        ? `${b.cap} lò ép${b.upgrade_price ? ` · thêm 2 lò ${b.upgrade_price} 🪙` : ""}`
                        : `Ép trái cây thành nước giá cao · cần cấp ${b.level}`
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
                  {b.id === "kitchen" || b.id === "juicer" ? "+2 lò" : "Nới"} · {b.upgrade_price}{" "}
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
                  {prod?.name.toLowerCase()} sau {formatGrow(effAnimalSec(k.time_sec, farm.perks))}
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
                <td>{formatGrow(effGrowSec(c.grow_sec, farm.perks))}</td>
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

/** Chữ riêng của từng công trình có lò (xưởng / máy ép nước). */
const CRAFT_COPY = {
  kitchen: {
    emoji: "🍳",
    name: "Xưởng chế biến",
    title: "Xưởng",
    slots: "Lò nấu",
    list: "Món chế biến",
    verb: "Nấu",
    intro:
      "Biến rau củ, trứng, sữa trong kho thành món ngon bán giá gấp rưỡi, xe tải còn đặt mua giá cao hơn. Có 2 lò, nâng lên 4 lò.",
    move: "Chọn chỗ khác trên bản đồ cho xưởng",
    unbuilt: "Chưa xây",
    busy: "Lò bận",
  },
  juicer: {
    emoji: "🧃",
    name: "Máy ép nước",
    title: "Máy ép",
    slots: "Lò ép",
    list: "Nước ép",
    verb: "Ép",
    intro:
      "Ép trái cây trong kho thành nước ép bán giá cao, xe tải cũng đặt mua. Có 2 lò, nâng lên 4 lò.",
    move: "Chọn chỗ khác trên bản đồ cho máy ép",
    unbuilt: "Chưa xây",
    busy: "Lò bận",
  },
} as const;

function KitchenPanel({
  building,
  farm,
  now,
  busy,
  onBuild,
  onMove,
  onUpgrade,
  onCraft,
  onFill,
  onCollect,
}: {
  building: "kitchen" | "juicer";
  farm: FarmView;
  now: number;
  busy: boolean;
  onBuild: () => void;
  onMove: () => void;
  onUpgrade: () => void;
  onCraft: (r: Recipe) => void;
  onFill: (r: Recipe) => void;
  onCollect: (slot: number | "all") => void;
}) {
  const copy = CRAFT_COPY[building];
  const k = farm.buildings?.find((b) => b.id === building);
  const recipes = (farm.recipes ?? []).filter(
    (r) => (r.building ?? "kitchen") === building,
  );
  if (!k || recipes.length === 0)
    return (
      <EmptyState icon="clock" title={`${copy.title} chưa mở`}>
        {copy.name} sẽ có sau khi máy chủ cập nhật.
      </EmptyState>
    );
  const balance = farm.wallet?.balance ?? 0;
  const stock = farm.storage?.stock ?? {};
  const items = new Map((farm.items ?? []).map((i) => [i.id, i]));
  const byRecipe = new Map(recipes.map((r) => [r.id, r]));
  const jobs = new Map(
    (farm.jobs ?? [])
      .filter((j) => (j.building ?? "kitchen") === building)
      .map((j) => [j.slot, j]),
  );
  const free = k.built ? k.cap - jobs.size : 0;
  const done = jobsReady(farm, now, building);
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
            <Emo e={copy.emoji} />
          </span>
          <div>
            <b>{copy.name}</b>
            <small>{copy.intro}</small>
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
              {copy.slots} {jobs.size}/{k.cap}
            </b>
            <span className="kitchen-head-actions">
              <button
                type="button"
                className="btn btn-sm"
                onClick={onMove}
                title={copy.move}
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

      <h3 className="farm-h">{copy.list}</h3>
      <ul className="recipe-list">
        {sorted.map((r) => {
          const locked = farm.level < r.level;
          const short = r.inputs.some((i) => (stock[i.item] ?? 0) < i.qty);
          const block = locked
            ? `Cấp ${r.level}`
            : !k.built
              ? copy.unbuilt
              : short
                ? "Thiếu"
                : free <= 0
                  ? copy.busy
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
              <span className="recipe-actions">
                <button
                  type="button"
                  className="btn btn-sm btn-primary"
                  disabled={!!block || busy}
                  onClick={() => onCraft(r)}
                >
                  {block ?? copy.verb}
                </button>
                {!block && free > 1 && (
                  <button
                    type="button"
                    className="btn btn-sm"
                    disabled={busy}
                    onClick={() => onFill(r)}
                    title="Cho món này vào mọi lò còn trống"
                  >
                    Làm đầy lò
                  </button>
                )}
              </span>
            </li>
          );
        })}
      </ul>
    </div>
  );
}

/* =========================================================
   Vườn cây ăn quả + máy ép nước
   ========================================================= */

function OrchardPanel({
  farm,
  now,
  busy,
  spot,
  onPick,
  onPlant,
  onHarvestAll,
  onChop,
  juicer,
}: {
  farm: FarmView;
  now: number;
  busy: boolean;
  spot: number | null;
  onPick: (spot: number | null) => void;
  onPlant: (spot: number, k: FruitTree) => void;
  onHarvestAll: () => void;
  onChop: (t: TreeView) => void;
  juicer: ReactNode;
}) {
  const kinds = farm.tree_kinds ?? [];
  const vouchers = farm.xmas?.tree_vouchers ?? 0; // phiếu cây giống Noel: lần trồng kế tiếp miễn phí
  const open = farm.orchard_spots ?? 0;
  const trees = farm.trees ?? [];
  const bySpot = new Map(trees.map((t) => [t.spot, t]));
  const kindOf = new Map(kinds.map((k) => [k.id, k]));
  const balance = farm.wallet?.balance ?? 0;
  const readyN = trees.filter((t) => treeLive(t, now).ready).length;
  // chỗ đã chọn (bấm trên bản đồ) nếu còn trống, không thì chỗ trống đầu tiên đã mở
  const isFree = (i: number) => i < open && !bySpot.has(i);
  const target =
    spot !== null && isFree(spot)
      ? spot
      : Array.from({ length: ORCHARD_SPOTS }, (_, i) => i).find(isFree);
  const sorted = [...kinds].sort((a, b) => a.level - b.level);
  const fruitCount = 3 + (farm.perks?.harvest_bonus ?? 0);
  return (
    <div className="farm-section">
      {kinds.length === 0 ? (
        <EmptyState icon="clock" title="Vườn cây chưa mở">
          Vườn cây ăn quả sẽ có sau khi máy chủ cập nhật.
        </EmptyState>
      ) : (
        <>
          <div className="kitchen-head">
            <b>
              Vườn cây {trees.length}/{open}
            </b>
            {readyN > 0 && (
              <button
                type="button"
                className="btn btn-sm btn-primary"
                disabled={busy}
                onClick={onHarvestAll}
              >
                {emo(`🧺 Hái ${readyN * fruitCount} quả`)}
              </button>
            )}
          </div>
          {open === 0 && (
            <p className="farm-hint">
              Vườn cây mở ở cấp {orchardUnlockLevel(0)}.
            </p>
          )}
          {trees.length > 0 && (
            <ul className="orchard-trees">
              {trees.map((t) => {
                const k = kindOf.get(t.tree);
                const live = treeLive(t, now);
                return (
                  <li key={t.spot} className={live.ready ? "is-ready" : ""}>
                    <span className="stock-emoji" aria-hidden>
                      <Emo e={k?.emoji ?? "🌳"} />
                    </span>
                    <div>
                      <b>{k?.name ?? t.tree}</b>
                      <small>
                        Chỗ {t.spot + 1} ·{" "}
                        {live.ready
                          ? "có quả, bấm cây để hái"
                          : t.state === "growing"
                            ? `lớn, còn ${formatLeft(live.left)}`
                            : `ra quả sau ${formatLeft(live.left)}`}
                      </small>
                    </div>
                    <button
                      type="button"
                      className="btn btn-sm"
                      disabled={busy}
                      onClick={() => onChop(t)}
                      title="Chặt cây"
                    >
                      <Emo e="🪓" /> Chặt
                    </button>
                  </li>
                );
              })}
            </ul>
          )}
          <h3 className="farm-h">
            Cây giống
            {target !== undefined && (
              <small className="farm-h-note"> · trồng vào chỗ {target + 1}</small>
            )}
          </h3>
          {open > 0 && target === undefined && (
            <p className="farm-hint">
              Hết chỗ trống.{" "}
              {open < ORCHARD_SPOTS &&
                `Mở thêm chỗ ở cấp ${orchardUnlockLevel(open)}.`}
            </p>
          )}
          <ul className="recipe-list">
            {sorted.map((k) => {
              const block =
                farm.level < k.level
                  ? `Cấp ${k.level}`
                  : open === 0
                    ? `Mở ở cấp ${orchardUnlockLevel(0)}`
                    : target === undefined
                      ? "Hết chỗ"
                      : balance < k.price && !vouchers
                        ? "Thiếu xu"
                        : null;
              return (
                <li
                  key={k.id}
                  className={`recipe-card${farm.level < k.level ? " is-locked" : ""}${!block ? " is-ready" : ""}`}
                >
                  <span className="stock-emoji" aria-hidden>
                    <Emo e={k.emoji} />
                  </span>
                  <div className="recipe-main">
                    <b>{k.name}</b>
                    <small>
                      <Emo e="⏱️" /> {formatGrow(effGrowSec(k.mature_sec, farm.perks))}{" "}
                      ra quả, rồi mỗi {formatGrow(effGrowSec(k.cycle_sec, farm.perks))}
                    </small>
                    <small>
                      {fruitCount} <Emo e={k.fruit_emoji} /> {k.fruit_name.toLowerCase()} mỗi lần ·{" "}
                      {effSellPrice(k.fruit_price, "fruit", farm.perks)}{" "}
                      <Emo e="🪙" />/quả · +{k.xp} XP
                    </small>
                  </div>
                  <button
                    type="button"
                    className="btn btn-sm btn-primary"
                    disabled={!!block || busy}
                    onClick={() => target !== undefined && onPlant(target, k)}
                  >
                    {emo(block ?? (vouchers ? "Trồng · Miễn phí 🎄" : `Trồng · ${k.price} 🪙`))}
                  </button>
                </li>
              );
            })}
          </ul>
          {spot !== null && (
            <button
              type="button"
              className="btn btn-sm orchard-clear"
              onClick={() => onPick(null)}
            >
              Bỏ chọn chỗ {spot + 1}
            </button>
          )}
        </>
      )}
      <h3 className="farm-h">Máy ép nước</h3>
      {juicer}
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
  const total = rows.reduce((n, i) => n + effSellTotal(i.price, i.kind, st.stock[i.id] ?? 0, farm.perks), 0);
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
                      ×{n} · {effSellPrice(i.price, i.kind, farm.perks)} <Emo e="🪙" />/cái
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
   Chợ đen: 10 chỗ chung toàn server, giá do người bán đặt
   ========================================================= */

function BlackMarketPanel({
  farm,
  now,
  busy,
  onList,
  onBuy,
  onCancel,
  onVisit,
}: {
  farm: FarmView;
  now: number;
  busy: boolean;
  onList: (item: string, qty: number, price: number) => void;
  onBuy: (l: MarketListing) => void;
  onCancel: (l: MarketListing) => void;
  onVisit: (id: number) => void;
}) {
  const [selling, setSelling] = useState(false);
  const m = farm.market;
  if (!m)
    return (
      <EmptyState icon="clock" title="Chợ đen chưa mở">
        Chợ đen sẽ có sau khi máy chủ cập nhật.
      </EmptyState>
    );
  const items = farm.items ?? [];
  const balance = farm.wallet?.balance ?? 0;
  const mineCount = m.listings.filter((l) => l.mine).length;
  const full = m.used >= m.slots;
  const capped = mineCount >= m.per_user;
  const free = Math.max(0, m.slots - m.used);
  return (
    <div className="farm-section">
      <div className={`storage-cap${full ? " is-full" : ""}`}>
        <div>
          <b>
            Chợ đen {m.used}/{m.slots} chỗ
          </b>
          <span className="quest-bar">
            <i style={{ width: `${(m.used / Math.max(1, m.slots)) * 100}%` }} />
          </span>
        </div>
        <button
          type="button"
          className="btn btn-sm btn-primary"
          disabled={busy || full || capped}
          title={
            full
              ? "Chợ đã kín chỗ"
              : capped
                ? `Mỗi người treo tối đa ${m.per_user} lô`
                : undefined
          }
          onClick={() => setSelling(true)}
        >
          Treo hàng
        </button>
      </div>
      <p className="farm-foot">
        Cả server dùng chung {m.slots} chỗ, kín chỗ thì không ai treo thêm được.
        Mỗi người tối đa {m.per_user} lô, lô treo quá 24 giờ tự về kho. Người
        mua trả đúng giá bạn đặt.
      </p>
      {m.listings.length === 0 ? (
        <EmptyState icon="clock" title="Chợ đen đang vắng">
          Chưa ai treo hàng. Treo món trong kho với giá bạn muốn để mọi người
          cùng thấy.
        </EmptyState>
      ) : (
        <ul className="stock-list market-list">
          {m.listings.map((l) => {
            const it = items.find((i) => i.id === l.item);
            const left = new Date(l.expires_at).getTime() - now;
            return (
              <li key={l.id} className={l.mine ? "is-mine" : undefined}>
                <span className="stock-emoji" aria-hidden>
                  <Emo e={it?.emoji ?? "📦"} />
                </span>
                <span className="stock-main">
                  <b>
                    {it?.name ?? l.item} ×{l.qty} ·{" "}
                    {l.price.toLocaleString("vi-VN")} <Emo e="🪙" />
                  </b>
                  <small>
                    <button
                      type="button"
                      className="market-seller"
                      onClick={() => !l.mine && onVisit(l.seller.id)}
                      disabled={l.mine}
                    >
                      {l.mine ? "Của bạn" : displayName(l.seller)}
                    </button>{" "}
                    · còn {formatLeft(left)}
                  </small>
                </span>
                {l.mine ? (
                  <button
                    type="button"
                    className="btn btn-sm"
                    disabled={busy}
                    onClick={() => onCancel(l)}
                  >
                    Gỡ
                  </button>
                ) : (
                  <button
                    type="button"
                    className="btn btn-sm btn-primary"
                    disabled={busy || balance < l.price}
                    title={balance < l.price ? "Không đủ xu" : undefined}
                    onClick={() => onBuy(l)}
                  >
                    Mua
                  </button>
                )}
              </li>
            );
          })}
        </ul>
      )}
      {free > 0 && m.listings.length > 0 && (
        <p className="farm-foot">Còn {free} chỗ trống.</p>
      )}
      {selling && (
        <MarketSellDialog
          farm={farm}
          busy={busy}
          onClose={() => setSelling(false)}
          onList={(item, qty, price) => {
            setSelling(false);
            onList(item, qty, price);
          }}
        />
      )}
    </div>
  );
}

function MarketSellDialog({
  farm,
  busy,
  onClose,
  onList,
}: {
  farm: FarmView;
  busy: boolean;
  onClose: () => void;
  onList: (item: string, qty: number, price: number) => void;
}) {
  const stock = farm.storage?.stock ?? {};
  const maxQty = farm.market?.max_qty ?? 50;
  const maxPrice = farm.market?.max_price ?? 1_000_000;
  const items = (farm.items ?? []).filter((i) => (stock[i.id] ?? 0) > 0);
  const [pick, setPick] = useState<string | null>(items[0]?.id ?? null);
  const [qty, setQty] = useState(1);
  const [price, setPrice] = useState(0);
  const it = items.find((i) => i.id === pick);
  const have = it ? Math.min(stock[it.id] ?? 0, maxQty) : 0;
  const n = Math.max(1, Math.min(qty, have));
  // giá tham khảo: bán ở kho được bao nhiêu
  const ref = it ? effSellTotal(it.price, it.kind, n, farm.perks) : 0;
  const ok = !!it && have > 0 && price >= 1 && price <= maxPrice;
  return (
    <Dialog title="Treo hàng lên chợ đen" onClose={onClose} className="farm-gift-dialog">
      {items.length === 0 ? (
        <p className="muted">Kho đang trống, chưa có gì để treo bán.</p>
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
            <div className="market-form">
              <label className="field">
                <span>
                  Số {it.name.toLowerCase()} <small>(có {stock[it.id]}, tối đa {have})</small>
                </span>
                <input
                  type="number"
                  min={1}
                  max={have}
                  value={qty || ""}
                  onChange={(e) =>
                    setQty(Math.max(0, Math.min(have, Math.floor(Number(e.target.value) || 0))))
                  }
                />
              </label>
              <label className="field">
                <span>
                  Giá cả lô (xu) <small>bán ở kho được {ref.toLocaleString("vi-VN")}</small>
                </span>
                <input
                  type="number"
                  min={1}
                  max={maxPrice}
                  value={price || ""}
                  placeholder={String(ref)}
                  onChange={(e) =>
                    setPrice(Math.max(0, Math.min(maxPrice, Math.floor(Number(e.target.value) || 0))))
                  }
                />
              </label>
            </div>
          )}
          <div className="farm-confirm-actions">
            <button type="button" className="btn" onClick={onClose}>
              Để sau
            </button>
            <button
              type="button"
              className="btn btn-primary"
              disabled={busy || !ok}
              onClick={() => it && onList(it.id, n, price)}
            >
              Treo {n} <Emo e={it?.emoji} /> · {price.toLocaleString("vi-VN")} <Emo e="🪙" />
            </button>
          </div>
        </>
      )}
    </Dialog>
  );
}

/* =========================================================
   Giáng sinh: đơn Ông già Noel, cửa hàng kẹo, tất Noel
   ========================================================= */

/** "20:00 ngày 24/12" (giờ máy người xem) */
const eveLabel = (iso: string) => {
  const d = new Date(iso);
  const p = (n: number) => String(n).padStart(2, "0");
  return `${p(d.getHours())}:${p(d.getMinutes())} ngày ${p(d.getDate())}/${p(d.getMonth() + 1)}`;
};

const viDate = (d: string) => d.split("-").reverse().slice(0, 2).join("/");

/* ---------- Giới thiệu sự kiện & thể lệ ---------- */

function XmasIntro({ x, onClose, onGo }: { x: XmasView; onClose: () => void; onGo: () => void }) {
  const rules: [string, string, ReactNode][] = [
    ["🎅", "Đơn của Ông già Noel", <>Mỗi ngày 3 đơn riêng, giao đủ hàng được Kẹo Noel và XP. Giao đủ cả 3 đơn liên tiếp nhiều ngày: thêm {x.streak_candy} kẹo × số ngày (tối đa {x.streak_max} ngày).</>],
    ["🎁", "Ông già Noel thả quà", <>4 lần mỗi ngày quà rơi ngẫu nhiên vào vườn một người, cả server được báo. Ai nhặt trước trong 10 phút được 10 kẹo + 100 xu.</>],
    ["⛄", "Ném cầu tuyết", <>Mùa Noel không trộm được! Thay vào đó ném cầu tuyết vào vườn hàng xóm (10 quả/ngày, hôm nay còn {x.snow_left}): trúng +1 kẹo, ném trả người vừa ném mình +2 nữa.</>],
    ["🧦", "Tất Noel", <>Treo tối đa 3 chiếc tất/ngày ở vườn bạn bè, bỏ món trong kho vào. Hôm sau chủ vườn mới mở và biết ai tặng. Cả hai cùng được kẹo.</>],
    ["🎄", "Cây thông chung", <>Cả server góp hàng vào một cây thông (mỗi ngày nhận tối đa {x.tree_candy_day} kẹo từ góp cây). Đạt mỗi mốc, ai đã góp từ {x.tree_min_share} điểm nhận {x.tree.reward} kẹo, cây thông trong vườn mọi người to và sáng thêm.</>],
    ["🎁", "Hộp quà bí ẩn", <>{x.box_cost} kẹo một hộp: xu, kẹo, đồ quý… hoặc đồ trang trí hiếm (tuần lộc, xe trượt tuyết…) đặt trong vườn cho cả server ngắm.</>],
    ["✨", "Giờ vàng đêm Noel", <>{x.golden_from}:00–{x.golden_to}:00 ngày {viDate(x.golden_day)}: mọi kẹo nhận được nhân đôi.</>],
    ["🏆", "Đua top kẹo", <>Top 10 người kiếm nhiều kẹo nhất có huy hiệu 🎄 Top Noel cạnh tên trong chat.</>],
    ["🌱", "Ưu đãi tân thủ", <>Người chơi dưới cấp {x.newbie_level}: mọi kẹo nhận được +{x.newbie_pct}%. Mới chơi vẫn đua top được!</>],
    ["🤫", "Ông già Noel bí mật", <>Ai trong nhóm cũng tham gia được, không giới hạn số người. Ngày {viDate(x.santa.draw_day)} bốc thăm mỗi người tặng quà bí mật cho đúng một người, vào muộn vẫn được ghép thêm tới trước đêm Noel. Đêm Noel mới lộ diện ai tặng ai!</>],
    ["🏅", "Mốc kẹo", <>Kiếm đủ {x.milestones.map((m) => m.need).join(" / ")} kẹo để nhận quà. Mốc cuối là 🌟 Ngôi sao Giáng sinh độc quyền.</>],
    ["🍪", "Món Noel", <>Xưởng chế biến có thêm Bánh gừng, Ca cao nóng, Kẹo gậy, chỉ nấu được trong mùa Noel.</>],
    ["🌙", "Đêm Noel", <>Từ {eveLabel(x.eve.at)}: Ông già Noel ghé vườn mọi người tặng quà lớn và 🔔 Chuông Đêm Thánh, lộ diện Ông già Noel bí mật.</>],
  ];
  return (
    <Dialog title="🎄 Lễ hội Giáng sinh ở Nông trại" onClose={onClose} className="xmas-intro">
      <p className="xmas-intro-lead">
        Từ <b>{viDate(x.from)}</b> đến <b>{viDate(x.to)}</b>, nông trại phủ tuyết trắng! Kiếm <b>Kẹo Noel</b>{" "}
        <Emo e="🍬" /> để đổi quà, mở hộp bí ẩn và leo top. Cửa hàng kẹo mở tới {viDate(x.shop_to)}.
      </p>
      <ul className="xmas-intro-rules">
        {rules.map(([e, title, body]) => (
          <li key={title}>
            <span aria-hidden>
              <Emo e={e} />
            </span>
            <div>
              <b>{title}</b>
              <p>{body}</p>
            </div>
          </li>
        ))}
      </ul>
      <div className="farm-confirm-actions">
        <button type="button" className="btn" onClick={onClose}>
          Để sau
        </button>
        <button type="button" className="btn btn-primary" data-autofocus onClick={onGo}>
          Chơi ngay 🎅
        </button>
      </div>
    </Dialog>
  );
}

/* ---------- Popup mở quà Noel ---------- */

type Reveal = {
  /** hình hộp trước khi mở */
  box: string;
  title: string;
  prizes: { emoji: string; label: string; sub?: string }[];
  rare?: boolean;
};

const itemOf = (f: FarmView, id?: string) => f.items?.find((i) => i.id === id);

function boxReveal(f: FarmView): Reveal | null {
  const p = f.gained?.prize;
  if (!p) return null;
  const r: Reveal = { box: "🎁", title: "Hộp quà bí ẩn", prizes: [], rare: p.rare };
  switch (p.kind) {
    case "decor": {
      const d = [...(f.xmas?.decors ?? []), ...(f.xmas?.special_decors ?? [])].find((x) => x.id === p.decor);
      r.prizes.push({ emoji: d?.emoji ?? "🎁", label: d?.name ?? "Đồ trang trí hiếm", sub: "Đã đặt trong vườn bạn, cả server đều thấy!" });
      break;
    }
    case "xu":
      r.prizes.push({ emoji: "🪙", label: `${p.xu?.toLocaleString("vi-VN")} xu` });
      break;
    case "candy":
      r.prizes.push({ emoji: "🍬", label: `${p.candy} Kẹo Noel` });
      break;
    case "item": {
      const it = itemOf(f, p.item);
      r.prizes.push({ emoji: it?.emoji ?? "📦", label: `${p.qty} ${it?.name.toLowerCase() ?? p.item}`, sub: "Đã cất vào kho" });
      break;
    }
  }
  return r;
}

function dropReveal(f: FarmView): Reveal | null {
  const g = f.gained;
  if (!g) return null;
  const prizes = [];
  if (g.candy) prizes.push({ emoji: "🍬", label: `${g.candy} Kẹo Noel` });
  if (g.xu) prizes.push({ emoji: "🪙", label: `${g.xu} xu` });
  return { box: "🎁", title: "Quà của Ông già Noel 🎅", prizes };
}

function socksReveal(f: FarmView): Reveal | null {
  const g = f.gained;
  if (!g?.socks?.length) return null;
  const prizes = g.socks.map((x) => {
    const it = itemOf(f, x.item);
    return { emoji: it?.emoji ?? "📦", label: `${x.qty} ${it?.name.toLowerCase() ?? x.item}`, sub: `từ ${displayName(x.from)}` };
  });
  if (g.candy) prizes.push({ emoji: "🍬", label: `${g.candy} Kẹo Noel`, sub: "thưởng mở tất" });
  return { box: "🧦", title: g.socks.length > 1 ? `Mở ${g.socks.length} chiếc tất Noel` : "Mở tất Noel", prizes };
}

const CONFETTI = ["🎉", "✨", "❄️", "🎊", "⭐", "🍬"];

/** Hộp rung lắc rồi bật mở, quà nảy ra kèm pháo giấy; hiếm thì có hào quang vàng. */
function XmasReveal({ reveal, onClose }: { reveal: Reveal; onClose: () => void }) {
  const [open, setOpen] = useState(false);
  useEffect(() => {
    const t = window.setTimeout(() => {
      setOpen(true);
      playCoinSound();
    }, 1100);
    return () => window.clearTimeout(t);
  }, []);
  return (
    <div className={`xmas-reveal${reveal.rare ? " is-rare" : ""}`} role="dialog" aria-modal aria-label={reveal.title} onClick={() => open && onClose()}>
      <div className="xmas-reveal-card" onClick={(e) => e.stopPropagation()}>
        <h3>{reveal.title}</h3>
        {!open ? (
          <div className="xmas-reveal-box" aria-label="Đang mở quà">
            <Emo e={reveal.box} />
          </div>
        ) : (
          <>
            {reveal.rare && <div className="xmas-reveal-ribbon">✨ HIẾM! ✨</div>}
            <div className="xmas-reveal-burst" aria-hidden>
              {Array.from({ length: 18 }, (_, i) => (
                <i
                  key={i}
                  style={
                    {
                      "--a": `${(i * 360) / 18}deg`,
                      "--d": `${90 + (i % 3) * 30}px`,
                      animationDelay: `${(i % 4) * 0.05}s`,
                    } as CSSProperties
                  }
                >
                  {CONFETTI[i % CONFETTI.length]}
                </i>
              ))}
            </div>
            <ul className="xmas-reveal-prizes">
              {reveal.prizes.map((p, i) => (
                <li key={i} style={{ animationDelay: `${0.1 + i * 0.18}s` }}>
                  <span className="xmas-reveal-emoji">
                    <Emo e={p.emoji} />
                  </span>
                  <b>{p.label}</b>
                  {p.sub && <small>{p.sub}</small>}
                </li>
              ))}
            </ul>
            <button type="button" className="btn btn-primary" autoFocus onClick={onClose}>
              Tuyệt vời!
            </button>
          </>
        )}
      </div>
    </div>
  );
}

function XmasPanel({
  farm,
  now,
  busy,
  onDeliver,
  onBuy,
  onOpenSocks,
  onClaimDrop,
  onVisit,
  onBox,
  onTree,
  onTreeClaim,
  onRules,
  onEveGift,
  onSantaJoin,
  onSantaSend,
  onSantaOpen,
  onMilestone,
}: {
  farm: FarmView;
  now: number;
  busy: boolean;
  onDeliver: (slot: number) => void;
  onBuy: (r: XmasReward) => void;
  onOpenSocks: () => void;
  onClaimDrop: (id: number) => void;
  onVisit: (id: number) => void;
  onBox: () => void;
  onTree: () => void;
  onTreeClaim: (goal: number) => void;
  onRules: () => void;
  onEveGift: () => void;
  onSantaJoin: () => void;
  onSantaSend: () => void;
  onSantaOpen: () => void;
  onMilestone: (n: number) => void;
}) {
  const x = farm.xmas!;
  const items = new Map((farm.items ?? []).map((i) => [i.id, i]));
  const stock = farm.storage?.stock ?? {};
  const left = x.orders.filter((o) => !o.done).length;
  const nextBonus = x.streak_candy * Math.min((left === 0 ? x.streak : x.streak + 1), x.streak_max);
  return (
    <div className="farm-section">
      <div className="storage-cap">
        <div>
          <b>
            <Emo e="🍬" /> {x.candy.toLocaleString("vi-VN")} Kẹo Noel
          </b>
          <small className="muted">
            {" "}
            · sự kiện {viDate(x.from)}–{viDate(x.to)}, đổi quà tới {viDate(x.shop_to)}
          </small>
        </div>
        <button type="button" className="btn btn-sm" onClick={onRules}>
          <Emo e="📜" /> Thể lệ
        </button>
      </div>
      {x.newbie && (
        <div className="xmas-newbie">
          <Emo e="🌱" /> <b>Ưu đãi tân thủ:</b> dưới cấp {x.newbie_level}, mọi kẹo bạn nhận được <b>+{x.newbie_pct}%</b>!
        </div>
      )}

      {x.golden ? (
        <div className="xmas-golden">
          <Emo e="✨" /> <b>Giờ vàng đêm Noel!</b> Mọi kẹo nhận được đang <b>x2</b> tới {x.golden_to}:00.
        </div>
      ) : (
        x.on && (
          <p className="farm-foot">
            <Emo e="✨" /> Giờ vàng {x.golden_from}:00–{x.golden_to}:00 ngày {viDate(x.golden_day)}: mọi kẹo x2.
          </p>
        )
      )}

      {x.eve.now ? (
        <div className="xmas-eve is-now">
          <b>🎄 Đêm Noel đã đến!</b> Ông già Noel ghé vườn mọi người.
          {x.eve.gift ? (
            <button type="button" className="btn btn-primary" disabled={busy} onClick={onEveGift}>
              🛷 Nhận quà đêm Noel
            </button>
          ) : (
            <small> Bạn đã nhận quà đêm Noel ✓</small>
          )}
        </div>
      ) : (
        x.on && (
          <div className="xmas-eve">
            <Emo e="🌙" /> <b>Đêm Noel</b> {eveLabel(x.eve.at)}{" "}
            · còn {formatLeft(new Date(x.eve.at).getTime() - now)}: quà cho mọi người, lộ diện Ông già Noel bí mật
          </div>
        )
      )}

      <h4>
        <Emo e="🤫" /> Ông già Noel bí mật
      </h4>
      {(() => {
        const s = x.santa;
        const items = new Map((farm.items ?? []).map((i) => [i.id, i]));
        if (s.gift_ready)
          return (
            <div className="order-card is-ready">
              <span>🎁 Có quà Ông già Noel bí mật cho bạn! Mở để biết ai tặng.</span>
              <button type="button" className="btn btn-sm btn-primary" disabled={busy} onClick={onSantaOpen}>
                Mở quà
              </button>
            </div>
          );
        if (s.can_join)
          return (
            <div className="xmas-santa">
              <p className="farm-foot">
                {s.drawn
                  ? `Đã bốc thăm cho ${s.people} người, đăng ký bây giờ vẫn được và vào vòng ngay!`
                  : `Ai trong nhóm cũng tham gia được. Ngày ${viDate(s.draw_day)} bốc thăm, mỗi người tặng bí mật đúng một người, vào muộn vẫn được ghép thêm.`}{" "}
                Đêm Noel mới lộ ai tặng ai! ({s.people} người đã tham gia)
              </p>
              <button type="button" className="btn btn-primary" disabled={busy} onClick={onSantaJoin}>
                🎅 Tham gia
              </button>
            </div>
          );
        if (!s.joined)
          return (
            <p className="farm-foot">
              {`Đêm Noel đã đến, hết đăng ký. ${s.people} người đã tham gia, xem ai tặng ai nhé!`}
            </p>
          );
        if (!s.target)
          return (
            <p className="farm-foot">
              {s.drawn
                ? `Đã đăng ký ✓ · Đang chờ thêm một người nữa để ghép cặp (${s.people} người tham gia).`
                : `Đã đăng ký ✓ · Bốc thăm ngày ${viDate(s.draw_day)} (${s.people} người tham gia).`}
            </p>
          );
        const it = s.sent_item ? items.get(s.sent_item) : undefined;
        return (
          <div className="xmas-santa">
            <p>
              Bạn là Ông già Noel bí mật của <b>{displayName(s.target)}</b> 🤫 Giữ kín nhé!
            </p>
            {s.sent ? (
              <p className="farm-foot">
                Đã gửi {s.sent_qty} <Emo e={it?.emoji ?? "📦"} /> {it?.name.toLowerCase()} · chờ đêm Noel lộ diện ({s.sent_count}/
                {s.people} người đã gửi)
              </p>
            ) : x.eve.revealed ? (
              <p className="farm-foot">Đêm Noel đã đến, không gửi quà được nữa.</p>
            ) : (
              <>
                <p className="farm-foot">
                  {s.sent_count}/{s.people} người đã gửi quà. Gửi trước đêm Noel, được +5 <Emo e="🍬" />.
                </p>
                <button type="button" className="btn btn-primary" disabled={busy} onClick={onSantaSend}>
                  🎁 Gửi quà bí mật
                </button>
              </>
            )}
          </div>
        );
      })()}

      <h4>
        <Emo e="🏅" /> Mốc kẹo của bạn
      </h4>
      {(() => {
        const last = x.milestones[x.milestones.length - 1]?.need ?? 1;
        return (
          <div className="xmas-tree">
            <span className="quest-bar xmas-tree-bar">
              <i style={{ width: `${Math.min(100, (x.earned / last) * 100)}%` }} />
              {x.milestones.map((m) => (
                <b key={m.need} style={{ left: `${(m.need / last) * 100}%` }} className={x.earned >= m.need ? "is-on" : ""} />
              ))}
            </span>
            <p className="farm-foot">
              Đã kiếm {x.earned} / {last} <Emo e="🍬" /> (kẹo đã tiêu vẫn tính)
            </p>
            <div className="xmas-goals">
              {x.milestones.map((m, i) => {
                const reached = x.earned >= m.need;
                const p = m.prize;
                const it = p.item ? farm.items?.find((t) => t.id === p.item) : undefined;
                const label =
                  p.kind === "xu"
                    ? `${p.xu} 🪙`
                    : p.kind === "item"
                      ? `${p.qty} ${it?.emoji ?? "📦"}`
                      : x.special_decors.find((d) => d.id === p.decor)?.emoji ?? "🌟";
                return (
                  <button
                    key={m.need}
                    type="button"
                    className={`btn btn-sm${reached && !m.claimed ? " btn-primary" : ""}`}
                    disabled={busy || !reached || m.claimed}
                    title={reached ? undefined : `Cần kiếm ${m.need} kẹo`}
                    onClick={() => onMilestone(i + 1)}
                  >
                    {m.need}: {m.claimed ? "✓" : label}
                  </button>
                );
              })}
            </div>
          </div>
        );
      })()}

      {x.drops.length > 0 && (
        <ul className="order-list">
          {x.drops.map((d) => (
            <li key={d.id} className="order-card is-ready xmas-drop-card">
              <span>
                <Emo e="🎅" /> Quà đang rơi ở vườn{" "}
                <button type="button" className="market-seller" onClick={() => onVisit(d.owner.id)}>
                  {d.owner.id === farm.user.id ? "của bạn" : displayName(d.owner)}
                </button>{" "}
                · còn {formatLeft(new Date(d.expires_at).getTime() - now)}
              </span>
              <button type="button" className="btn btn-sm btn-primary" disabled={busy} onClick={() => onClaimDrop(d.id)}>
                Nhặt ngay
              </button>
            </li>
          ))}
        </ul>
      )}

      {(x.socks_ready > 0 || x.socks_waiting > 0) && (
        <div className="order-card is-ready">
          <span>
            <Emo e="🧦" />{" "}
            {x.socks_ready > 0
              ? `Có ${x.socks_ready} chiếc tất đang chờ bạn mở!`
              : `${x.socks_waiting} chiếc tất vừa được treo, mai mới mở được.`}
          </span>
          {x.socks_ready > 0 && (
            <button type="button" className="btn btn-sm btn-primary" disabled={busy} onClick={onOpenSocks}>
              Mở tất
            </button>
          )}
        </div>
      )}

      {x.on && (
        <>
          <h4>
            <Emo e="🎅" /> Đơn của Ông già Noel hôm nay
          </h4>
          <p className="farm-foot">
            Giao đủ 3 đơn để nhận thêm {nextBonus} <Emo e="🍬" /> (chuỗi {x.streak} ngày, mỗi ngày liên
            tiếp +{x.streak_candy}, tối đa {x.streak_max} ngày).
          </p>
          <ul className="order-list">
            {x.orders.map((o) => (
              <li
                key={o.slot}
                className={`order-card${o.can_deliver ? " is-ready" : ""}${o.done ? " is-empty" : ""}`}
              >
                <ul className="order-items">
                  {o.items.map((it) => {
                    const have = stock[it.item] ?? 0;
                    const info = items.get(it.item);
                    return (
                      <li key={it.item} className={o.done || have >= it.qty ? "is-ok" : ""} title={info?.name}>
                        <Emo e={info?.emoji ?? "📦"} />
                        <b>{o.done ? it.qty : `${Math.min(have, it.qty)}/${it.qty}`}</b>
                      </li>
                    );
                  })}
                </ul>
                <div className="order-foot">
                  <span className="order-reward">
                    +{o.candy}
                    {x.newbie && (
                      <small className="xmas-newbie-plus" title="Ưu đãi tân thủ">
                        +{Math.ceil((o.candy * x.newbie_pct) / 100)}
                      </small>
                    )}{" "}
                    <Emo e="🍬" /> · +{o.xu} <Emo e="🪙" /> · +{o.xp} XP
                  </span>
                  {o.done ? (
                    <span className="muted">
                      Đã giao <Emo e="✅" />
                    </span>
                  ) : (
                    <button
                      type="button"
                      className="btn btn-sm btn-primary"
                      disabled={busy || !o.can_deliver}
                      onClick={() => onDeliver(o.slot)}
                    >
                      Giao
                    </button>
                  )}
                </div>
              </li>
            ))}
          </ul>
          <p className="farm-foot">
            <Emo e="🧦" /> Ghé vườn hàng xóm để treo tất Noel (còn {x.socks_left} tất) hoặc{" "}
            <Emo e="⛄" /> ném cầu tuyết (còn {x.snow_left} quả, trúng +1 kẹo, ném trả người vừa ném mình +2
            nữa). Mùa Giáng sinh không ai trộm được vườn ai.
          </p>

          <h4>
            <Emo e="🎄" /> Cây thông chung cả server
          </h4>
          {(() => {
            const t = x.tree;
            const last = t.goals[t.goals.length - 1] ?? 1;
            return (
              <div className="xmas-tree">
                <span className="quest-bar xmas-tree-bar">
                  <i style={{ width: `${Math.min(100, (t.total / last) * 100)}%` }} />
                  {t.goals.map((g) => (
                    <b key={g} style={{ left: `${(g / last) * 100}%` }} className={t.total >= g ? "is-on" : ""} />
                  ))}
                </span>
                <p className="farm-foot">
                  {t.total.toLocaleString("vi-VN")} / {last.toLocaleString("vi-VN")} điểm · bạn đã góp{" "}
                  {t.mine.toLocaleString("vi-VN")}
                  {t.mine < x.tree_min_share && ` (góp đủ ${x.tree_min_share} điểm để nhận thưởng mốc)`}
                </p>
                <p className="farm-foot">
                  Kẹo từ góp cây hôm nay còn {x.tree_candy_left}/{x.tree_candy_day} <Emo e="🍬" />. Hết phần kẹo vẫn góp
                  được để đẩy cây lên mốc.
                </p>
                <div className="xmas-goals">
                  {t.goals.map((g, i) => {
                    const reached = t.total >= g;
                    return (
                      <button
                        key={g}
                        type="button"
                        className={`btn btn-sm${reached && !t.claimed[i] && t.mine > 0 ? " btn-primary" : ""}`}
                        disabled={busy || !reached || t.claimed[i] || t.mine < x.tree_min_share}
                        title={
                          !reached
                            ? `Cần ${g.toLocaleString("vi-VN")} điểm`
                            : t.mine < x.tree_min_share
                              ? `Góp ít nhất ${x.tree_min_share} điểm để nhận`
                              : undefined
                        }
                        onClick={() => onTreeClaim(i + 1)}
                      >
                        Mốc {i + 1}: {t.claimed[i] ? "✓" : `${t.reward} 🍬`}
                      </button>
                    );
                  })}
                </div>
                <button type="button" className="btn btn-sm btn-primary" disabled={busy} onClick={onTree}>
                  Góp hàng
                </button>
                {t.top.length > 0 && (
                  <p className="farm-foot">
                    Góp nhiều nhất: {t.top.map((r) => `${displayName(r.user)} (${r.value.toLocaleString("vi-VN")})`).join(", ")}
                  </p>
                )}
              </div>
            );
          })()}
        </>
      )}

      {x.shop_open && (
        <>
          <h4>
            <Emo e="🎁" /> Hộp quà bí ẩn
          </h4>
          <p className="farm-foot">
            Có thể trúng xu, kẹo, len, nấm, áo len… hoặc <b>đồ trang trí hiếm</b> đặt trong vườn cho cả server
            ngắm:
          </p>
          <ul className="xmas-decor-list">
            {[...x.decors, ...x.special_decors].map((d) => {
              const has = farm.xmas_decor?.includes(d.id);
              return (
                <li key={d.id} className={has ? "is-on" : ""} title={has ? `Đã có: ${d.name}` : d.name}>
                  <Emo e={d.emoji} />
                </li>
              );
            })}
          </ul>
          <button type="button" className="btn btn-primary" disabled={busy || x.candy < x.box_cost} onClick={onBox}>
            Mở hộp · {x.box_cost} <Emo e="🍬" />
          </button>
        </>
      )}

      {x.top.length > 0 && (
        <>
          <h4>
            <Emo e="🏆" /> Top kẹo Noel
          </h4>
          <p className="farm-foot">Top {x.top.length} có huy hiệu 🎄 cạnh tên trong chat. Bạn đã kiếm {x.earned} kẹo.</p>
          <ol className="xmas-top">
            {x.top.map((r, i) => (
              <li key={r.user.id} className={r.user.id === farm.user.id ? "is-me" : undefined}>
                <span>{i < 3 ? ["🥇", "🥈", "🥉"][i] : `${i + 1}.`}</span>
                <button type="button" className="market-seller" onClick={() => onVisit(r.user.id)}>
                  {displayName(r.user)}
                </button>
                <b>
                  {r.value} <Emo e="🍬" />
                </b>
              </li>
            ))}
          </ol>
        </>
      )}

      {x.shop_open && (
        <>
          <h4>
            <Emo e="🎁" /> Đổi kẹo lấy quà
          </h4>
          {x.tree_vouchers > 0 && (
            <p className="farm-foot">
              <Emo e="🌳" /> Bạn có {x.tree_vouchers} phiếu cây giống: lần trồng cây ăn quả kế tiếp miễn phí.
            </p>
          )}
          <ul className="stock-list">
            {x.shop.map((r) => {
              const left = r.daily_max ? (x.shop_left[r.id] ?? 0) : undefined;
              return (
                <li key={r.id}>
                  <span className="stock-emoji" aria-hidden>
                    <Emo e={r.emoji} />
                  </span>
                  <span className="stock-main">
                    <b>{r.name}</b>
                    <small>
                      {r.cost} <Emo e="🍬" />
                      {left !== undefined && ` · hôm nay còn ${left}/${r.daily_max}`}
                      {r.note && ` · ${r.note}`}
                    </small>
                  </span>
                  <button
                    type="button"
                    className="btn btn-sm btn-primary"
                    disabled={busy || x.candy < r.cost || left === 0}
                    title={left === 0 ? "Mai đổi tiếp nhé" : x.candy < r.cost ? "Chưa đủ kẹo" : undefined}
                    onClick={() => onBuy(r)}
                  >
                    Đổi
                  </button>
                </li>
              );
            })}
          </ul>
        </>
      )}
    </div>
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
    case "market_sold": {
      const it = items.find((i) => i.id === l.crop);
      return `🕶️ ${who} mua lô ${it?.emoji ?? "📦"} ${it?.name.toLowerCase() ?? ""} của bạn ở chợ đen, bạn được ${(l.xu ?? 0).toLocaleString("vi-VN")} xu`;
    }
    case "snowball":
      return `⛄ ${who} ném cầu tuyết trúng vườn bạn! Ném trả trong 30 phút được thêm kẹo`;
    case "drop_claimed":
      return `🎅 ${who} đã nhặt quà Ông già Noel ở vườn bạn (bạn được 3 kẹo)`;
    case "sock_opened": {
      const it = items.find((i) => i.id === l.crop);
      return `🧦 ${who} đã mở tất Noel bạn treo: ${l.xu ?? 1} ${it?.emoji ?? "📦"} ${it?.name.toLowerCase() ?? ""}`.trim();
    }
    case "market_expired": {
      const it = items.find((i) => i.id === l.crop);
      return `⏰ Lô ${l.xu ?? 1} ${it?.emoji ?? "📦"} ${it?.name.toLowerCase() ?? ""} ở chợ đen hết hạn, đã về kho`;
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

/** Cửa sổ nhỏ chọn cây và công thức cho chế độ tự động. */
function AutoSettings({
  view,
  value,
  onChange,
  onClose,
}: {
  view: FarmView;
  value: FarmAutoSettings;
  onChange: (p: Partial<FarmAutoSettings>) => void;
  onClose: () => void;
}) {
  const crops = view.crops.filter((c) => c.level <= view.level);
  const recipes = (b: "kitchen" | "juicer") =>
    (view.recipes ?? []).filter(
      (r) => (r.building ?? "kitchen") === b && r.level <= view.level,
    );
  const sel = (
    label: string,
    v: string | undefined,
    opts: { id: string; name: string; emoji: string }[],
    set: (id: string | undefined) => void,
  ) => (
    <label className="farm-auto-field">
      <span>{label}</span>
      <select value={v ?? ""} onChange={(e) => set(e.target.value || undefined)}>
        <option value="">— không —</option>
        {opts.map((o) => (
          <option key={o.id} value={o.id}>
            {o.emoji} {o.name}
          </option>
        ))}
      </select>
    </label>
  );
  return (
    <div className="farm-auto-pop" role="dialog" aria-label="Cài đặt tự động">
      {sel("Cây gieo", value.crop, crops, (id) => onChange({ crop: id }))}
      {sel("Món ở xưởng", value.kitchen_recipe, recipes("kitchen"), (id) =>
        onChange({ kitchen_recipe: id }),
      )}
      {sel("Món ở máy ép", value.juicer_recipe, recipes("juicer"), (id) =>
        onChange({ juicer_recipe: id }),
      )}
      <label className="farm-auto-check">
        <input
          type="checkbox"
          checked={value.buy_feed}
          onChange={(e) => onChange({ buy_feed: e.target.checked })}
        />
        Tự mua thức ăn khi thiếu
      </label>
      <button type="button" className="btn btn-sm" onClick={onClose}>
        Xong
      </button>
    </div>
  );
}
