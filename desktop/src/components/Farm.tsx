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
  farmScreen,
  formatGrow,
  formatLeft,
  useFarmOpen,
  type Crop,
  type FarmLog,
  type FarmView,
  type LeaderRow,
  type Neighbor,
  type Plot,
  type PlotState,
  type Quest,
} from "../lib/farm";
import { playCoinSound } from "../lib/sound";
import { useChat } from "../state/ChatProvider";
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

type Tab = "quests" | "neighbors" | "shop" | "log" | "top";
const TABS: { id: Tab; label: string; icon: string }[] = [
  { id: "quests", label: "Nhiệm vụ", icon: "📋" },
  { id: "neighbors", label: "Hàng xóm", icon: "🏡" },
  { id: "shop", label: "Cửa hàng", icon: "🛒" },
  { id: "log", label: "Nhật ký", icon: "📜" },
  { id: "top", label: "Xếp hạng", icon: "🏆" },
];

type Pop = {
  id: number;
  plot: number;
  text: string;
  tone: "xu" | "xp" | "bad" | "info";
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
  const view = visit ?? mine;
  const now = useServerNow(view?.now);

  const showToast = useCallback(
    (text: string, tone: "good" | "bad" | "level" = "good") => {
      setToast({ text, tone });
      window.setTimeout(
        () => setToast((t) => (t?.text === text ? null : t)),
        tone === "level" ? 3600 : 2600,
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
          setMine(f);
          setError(null);
        })
        .catch((err) => setError(errMsg(err))),
    [api],
  );

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
          logText(l, mine?.crops ?? []),
          l.action === "steal" ? "bad" : "good",
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
        if (g.xu)
          pop(at, `${g.xu > 0 ? "+" : ""}${g.xu} 🪙`, g.xu > 0 ? "xu" : "bad");
        if (g.xp) pop(at, `+${g.xp} XP`, "xp", 180);
        if (g.xu > 0) playCoinSound();
      }
      if (g?.level_up)
        showToast(
          `🎉 Lên cấp ${g.level_up}! Mở khoá cây mới trong cửa hàng hạt.`,
          "level",
        );
      else if (okText) showToast(okText);
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
            <div className="farm-owner">
              <Avatar user={view.user} size={38} />
              <div>
                <p className="farm-owner-name">
                  {view.me
                    ? "Nông trại của bạn"
                    : `Nông trại của ${displayName(view.user)}`}
                </p>
                <LevelBar view={view} />
              </div>
            </div>
          )}
          <div className="farm-hud-right">
            {mine?.wallet && (
              <span className="farm-coins" title="Số xu của bạn">
                <span aria-hidden>🪙</span>
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
              >
                🏠 <span>Về nhà</span>
              </button>
            )}
          </div>
        </header>

        <div className="farm-body">
          <section className="farm-field" aria-label="Cánh đồng">
            <Scenery tod={tod} dog={dogLeft > 0} />
            {view ? (
              <>
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
                        🧺 Thu hoạch tất cả <b>{ripeCount}</b>
                      </button>
                    ) : (
                      <span className="farm-tip">{fieldTip(view, now)}</span>
                    )
                  ) : (
                    <span className="farm-tip">
                      {view.steals_left !== undefined && (
                        <>Còn {view.steals_left} lượt trộm hôm nay · </>
                      )}
                      Chạm ô chín để <b>trộm</b>, ô đang lớn để <b>tưới giúp</b>
                      .
                    </span>
                  )}
                </div>
                <div className="farm-iso">
                  <div className="farm-world">
                    <WorldDecor dog={dogLeft > 0} />
                    <div
                      className="farm-plots"
                      style={
                        {
                          "--cols": view.max_plots > 9 ? 4 : 3,
                          "--rows": Math.ceil(
                            (view.me ? view.max_plots : view.plot_count) /
                              (view.max_plots > 9 ? 4 : 3),
                          ),
                        } as CSSProperties
                      }
                    >
                      {view.plots.map((p) => (
                        <PlotTile
                          key={p.index}
                          plot={p}
                          view={view}
                          now={now}
                          busy={busy}
                          pops={pops.filter((x) => x.plot === p.index)}
                          onClick={() => onPlot(p)}
                        />
                      ))}
                      {view.me &&
                        Array.from(
                          { length: view.max_plots - view.plot_count },
                          (_, i) => (
                            <button
                              key={`lock${i}`}
                              type="button"
                              className="plot is-locked"
                              onClick={() => setTab("shop")}
                              aria-label="Ô đất chưa mở, mua ở cửa hàng"
                            >
                              <span className="plot-soil" aria-hidden />
                              <span className="plot-stand" aria-hidden>
                                <span className="plot-lock">
                                  {i === 0 ? "🪧" : "🌿"}
                                </span>
                                {i === 0 && view.next_plot && (
                                  <span className="plot-tag">
                                    <span className="plot-meta">
                                      Mở ô: {view.next_plot.price} 🪙 · cấp{" "}
                                      {view.next_plot.level}
                                    </span>
                                  </span>
                                )}
                              </span>
                            </button>
                          ),
                        )}
                    </div>
                  </div>
                  {pops
                    .filter((x) => x.plot < 0)
                    .map((x) => (
                      <span key={x.id} className={`farm-pop is-${x.tone}`}>
                        {x.text}
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
                {toast.text}
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
                  <span aria-hidden>{t.icon}</span>
                  <span>{t.label}</span>
                  {t.id === "log" && newLogs > 0 && (
                    <i className="farm-tab-dot" />
                  )}
                  {t.id === "quests" && mine && questsReady(mine) > 0 && (
                    <i className="farm-tab-count">{questsReady(mine)}</i>
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
                  onVisit={goVisit}
                  visiting={visit?.user.id ?? null}
                />
              )}
              {tab === "shop" && mine && (
                <ShopPanel
                  farm={mine}
                  now={now}
                  busy={busy}
                  onBuyPlot={() =>
                    void act(
                      -1,
                      () => api.farmBuyPlot(),
                      "Đã mở thêm một ô đất 🌱",
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
                <LeaderPanel meId={state.me.id} onVisit={goVisit} />
              )}
              {!mine && tab !== "neighbors" && tab !== "top" && (
                <p className="muted">Đang tải…</p>
              )}
            </div>
          </aside>
        </div>
      </div>

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

/* =========================================================
   Cảnh vật: trời theo giờ, mặt trời / trăng, mây, đồi, hàng rào
   ========================================================= */

function Scenery({ tod }: { tod: string; dog: boolean }) {
  // ánh sáng theo giờ phủ lên cả thế giới (bình minh cam, hoàng hôn tím, đêm xanh thẫm)
  return <div className={`farm-tint is-${tod}`} aria-hidden />;
}

/** Một vật đứng trên mặt đất (cây, thú, nhà…): x, y là % mặt đất, s là cỡ (rem). */
function W({
  x,
  y,
  s = 2.4,
  c,
  anim,
  children,
}: {
  x: number;
  y: number;
  s?: number;
  c?: string;
  anim?: string;
  children: ReactNode;
}) {
  return (
    <i
      className={`w${c ? ` ${c}` : ""}`}
      style={
        {
          left: `${x}%`,
          top: `${y}%`,
          fontSize: `${s}rem`,
          animationDelay: anim,
        } as CSSProperties
      }
    >
      {children}
    </i>
  );
}

const ORCHARD: [number, number, string][] = [
  [80, 30, "🌳"],
  [87, 30, "🌳"],
  [94, 30, "🌳"],
  [80, 40, "🌳"],
  [87, 40, "🌳"],
  [94, 40, "🌳"],
  [80, 50, "🌳"],
  [87, 50, "🌳"],
  [94, 50, "🌳"],
];
const BORDER: [number, number, string, number][] = Array.from(
  { length: 16 },
  (_, i) => [
    3 + i * 6.3,
    i % 2 ? 4 : 7,
    i % 3 === 0 ? "🌲" : "🌳",
    i % 2 ? 3.4 : 3,
  ],
);
const FLOWERS = ["🌷", "🌻", "🌼", "🌷", "🌸", "🌻"];

/**
 * Thế giới nông trại quanh ruộng (kiểu FarmVille): hàng cây bao quanh, nhà kho, silo, chuồng
 * cừu/bò rào gỗ, chuồng gà, vườn cây ăn quả, luống hoa, ao vịt, đường đá. Ruộng nằm giữa.
 */
function WorldDecor({ dog }: { dog: boolean }) {
  return (
    <div className="world-decor" aria-hidden>
      <span className="g-patch p1" />
      <span className="g-patch p2" />
      <span className="g-patch p3" />
      <span className="g-path path-h" />
      <span className="g-path path-v" />
      <span className="g-pond" />
      <span className="g-pen pen-animals" />
      <span className="g-pen pen-chicks" />
      <span className="g-bed bed-1" />
      <span className="g-bed bed-2" />

      {BORDER.map(([x, y, e, s], i) => (
        <W key={`b${i}`} x={x} y={y} s={s}>
          {e}
        </W>
      ))}
      <W x={30} y={18} s={1} c="w-barn">
        <span className="farm-barn">
          <span className="farm-barn-roof" />
          <span className="farm-barn-door" />
          <span className="farm-barn-window" />
        </span>
      </W>
      <W x={38.5} y={17} s={1} c="w-silo">
        <span className="farm-silo" />
      </W>
      <W x={44} y={19} s={2.2}>
        🌾
      </W>
      <W x={47} y={20} s={1.9}>
        🌾
      </W>
      <W x={24} y={21} s={1.8}>
        🪵
      </W>

      {/* chuồng cừu, bò */}
      <W x={8} y={36} s={2.6} c="w-graze">
        🐄
      </W>
      <W x={15} y={33} s={2} c="w-graze" anim="-1.2s">
        🐑
      </W>
      <W x={19} y={39} s={2} c="w-graze" anim="-2.4s">
        🐑
      </W>
      <W x={11} y={45} s={2} c="w-graze" anim="-0.6s">
        🐑
      </W>
      <W x={18} y={48} s={2.2} c="w-graze" anim="-1.8s">
        🐖
      </W>
      <W x={6} y={49} s={1.9} c="w-graze" anim="-3s">
        🐐
      </W>

      {/* chuồng gà */}
      <W x={12} y={66} s={2.6}>
        🛖
      </W>
      <W x={8} y={74} s={1.6} c="w-walk">
        🐔
      </W>
      <W x={15} y={78} s={1.3} c="w-walk" anim="-3s">
        🐥
      </W>
      <W x={19} y={71} s={1.6} c="w-walk" anim="-6s">
        🐓
      </W>
      <W x={6} y={82} s={1.2} c="w-walk" anim="-1.5s">
        🐥
      </W>

      {/* vườn cây ăn quả */}
      {ORCHARD.map(([x, y, e], i) => (
        <W key={`o${i}`} x={x} y={y} s={2.7}>
          {e}
          <b className="w-fruit">{i % 3 === 1 ? "🍊" : "🍎"}</b>
        </W>
      ))}

      {/* ao vịt */}
      <W x={92} y={68} s={1.5} c="w-swim">
        🦆
      </W>
      <W x={95} y={72} s={1.3} c="w-swim" anim="-2s">
        🦆
      </W>
      <W x={86} y={60} s={1.6}>
        🪨
      </W>

      {/* luống hoa */}
      {Array.from({ length: 14 }, (_, i) => (
        <W
          key={`f${i}`}
          x={30 + (i % 7) * 2.6}
          y={84 + Math.floor(i / 7) * 4}
          s={1.4}
        >
          {FLOWERS[i % FLOWERS.length]}
        </W>
      ))}
      {Array.from({ length: 12 }, (_, i) => (
        <W
          key={`g${i}`}
          x={56 + (i % 6) * 2.8}
          y={84 + Math.floor(i / 6) * 4}
          s={1.4}
        >
          {FLOWERS[(i + 2) % FLOWERS.length]}
        </W>
      ))}

      {/* ruộng lúa, ruộng rau trang trí cho dày như nông trại thật */}
      {Array.from({ length: 18 }, (_, i) => (
        <W key={`w${i}`} x={58.5 + (i % 6) * 2.4} y={13 + Math.floor(i / 6) * 3.6} s={1.7}>
          🌾
        </W>
      ))}
      {Array.from({ length: 12 }, (_, i) => (
        <W key={`c${i}`} x={73 + (i % 4) * 2.6} y={62 + Math.floor(i / 4) * 3.4} s={1.5}>
          {i % 2 ? "🥬" : "🌽"}
        </W>
      ))}
      <W x={27} y={60} s={2.8} c="w-idle">
        🧑‍🌾
      </W>
      <W x={70} y={24} s={2}>
        🧺
      </W>
      {dog && (
        <W x={71} y={58} s={2.3} c="w-wag">
          🐕
        </W>
      )}
    </div>
  );
}

/* =========================================================
   Một ô đất
   ========================================================= */

function cropOf(view: FarmView, id?: string): Crop | undefined {
  return view.crops.find((c) => c.id === id);
}

function PlotTile({
  plot,
  view,
  now,
  busy,
  pops,
  onClick,
}: {
  plot: Plot;
  view: FarmView;
  now: number;
  busy: boolean;
  pops: Pop[];
  onClick: () => void;
}) {
  const live = liveState(plot, now);
  const crop = cropOf(view, plot.crop);
  const st = live.state;
  // cây lớn dần: mầm -> lá -> quả nhỏ -> quả chín
  const glyph =
    st === "empty"
      ? null
      : st === "withered"
        ? "🥀"
        : st === "ripe"
          ? crop?.emoji
          : live.progress < 0.34
            ? "🌱"
            : live.progress < 0.67
              ? "🌿"
              : crop?.emoji;
  const growScale =
    st === "growing" && live.progress >= 0.67
      ? 0.62 + (live.progress - 0.67) * 0.9
      : 1;
  const action = plotAction(plot, st, view);
  const label =
    st === "empty"
      ? view.me
        ? "Gieo hạt"
        : "Đất trống"
      : st === "ripe"
        ? view.me
          ? "Thu hoạch"
          : plot.can_steal
            ? "Trộm!"
            : "Đã chín"
        : st === "withered"
          ? view.me
            ? "Dọn ô"
            : "Héo rồi"
          : formatLeft(live.left);
  const cls = [
    "plot",
    `is-${st}`,
    plot.bug && st === "growing" && "has-bug",
    plot.waters > 0 && st === "growing" && "is-wet",
    action && "is-actionable",
    st === "ripe" && !view.me && plot.can_steal && "is-stealable",
  ]
    .filter(Boolean)
    .join(" ");
  return (
    <button
      type="button"
      className={cls}
      onClick={onClick}
      disabled={busy && st !== "empty"}
      aria-label={`Ô ${plot.index + 1}: ${crop ? crop.name : "trống"}, ${label}${plot.bug ? ", có sâu" : ""}`}
    >
      <span className="plot-soil" aria-hidden />
      {/* một thửa là cả khóm cây: mỗi cây dựng đứng ở một điểm trên mặt ruộng */}
      {glyph &&
        plantSpots(plot.crop).map(([x, y], i) => (
          <span
            key={i}
            className="plot-stand"
            style={
              {
                left: `${50 + x * 100}%`,
                top: `${50 + y * 100}%`,
              } as CSSProperties
            }
            aria-hidden
          >
            <span
              className={`plot-crop${st === "growing" && live.progress < 0.67 ? " is-sprout" : ""}${
                plantSpots(plot.crop).length > 4 ? " is-small" : ""
              }`}
              style={
                {
                  "--grow": growScale,
                  animationDelay: `${(i % 3) * -0.4}s`,
                } as CSSProperties
              }
            >
              {glyph}
            </span>
          </span>
        ))}
      {/* nhãn, sâu, nước… dựng ở giữa thửa */}
      <span className="plot-stand" aria-hidden>
        {st === "ripe" && <span className="plot-sparkle">✨</span>}
        {plot.bug && st === "growing" && <span className="plot-bug">🐛</span>}
        {plot.waters > 0 && st === "growing" && (
          <span className="plot-drops">
            {"💧".repeat(Math.min(3, plot.waters))}
          </span>
        )}
        {st === "empty" && view.me && <span className="plot-plus">+</span>}
        <span className="plot-tag">
          {st === "growing" && (
            <span className="plot-bar">
              <i style={{ width: `${Math.round(live.progress * 100)}%` }} />
            </span>
          )}
          <span className="plot-meta">{label}</span>
        </span>
        {action && <span className="plot-action">{action}</span>}
        {pops.map((x) => (
          <span key={x.id} className={`farm-pop is-${x.tone}`}>
            {x.text}
          </span>
        ))}
      </span>
    </button>
  );
}

/** Vị trí các cây trên một thửa (lệch khỏi tâm, theo tỉ lệ cạnh thửa). Cây to (dưa, bí) 2×2, còn lại 3×3. */
const SPOTS_3: [number, number][] = [-0.28, 0, 0.28].flatMap((y) =>
  [-0.28, 0, 0.28].map((x) => [x, y] as [number, number]),
);
const SPOTS_2: [number, number][] = [-0.2, 0.2].flatMap((y) =>
  [-0.2, 0.2].map((x) => [x, y] as [number, number]),
);
function plantSpots(crop?: string): [number, number][] {
  return crop === "dua_hau" || crop === "bi_ngo" ? SPOTS_2 : SPOTS_3;
}

/** Nút gợi ý nổi trên ô khi rê chuột: việc sẽ xảy ra khi bấm. */
function plotAction(p: Plot, st: PlotState, view: FarmView): string | null {
  if (view.me) {
    if (st === "ripe") return "🧺 Hái";
    if (st === "withered") return "🧹 Dọn";
    if (st === "empty") return "🌱 Gieo";
    if (p.bug) return "🐛 Bắt sâu";
    if (p.can_water) return "💧 Tưới";
    return null;
  }
  if (st === "ripe" && p.can_steal) return "🫣 Trộm";
  if (p.bug && p.can_bug) return "🐛 Bắt sâu";
  if (st === "growing" && p.can_water) return "💧 Tưới giúp";
  return null;
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
        Vụ tới chín sau <b>{formatLeft(growing[0].left)}</b>
        {empty > 0 && <> · còn {empty} ô trống</>}
      </>
    );
  if (empty > 0) return <>Chạm vào ô đất để gieo hạt 🌱</>;
  return "Ruộng đầy rồi, chờ cây lớn nhé.";
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
                  {locked ? "🔒" : c.emoji}
                </span>
                <b>{c.name}</b>
                <span className="seed-time">⏱ {formatGrow(c.grow_sec)}</span>
                {locked ? (
                  <span className="seed-lock">Mở ở cấp {c.level}</span>
                ) : (
                  <span className="seed-money">
                    <span>{c.seed} 🪙</span>
                    <span aria-hidden>→</span>
                    <span className="seed-sell">{c.sell} 🪙</span>
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
        Đang có <b>{balance.toLocaleString("vi-VN")} 🪙</b>. Cây chín để quá 12
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
            {farm.chest.claimed ? "📭" : "🎁"}
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
              Xong cả 4 nhiệm vụ: +{farm.chest.xu} 🪙 +{farm.chest.xp} XP
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
        {QUEST_ICON[q.id] ?? "⭐"}
      </span>
      <div className="quest-main">
        <b>{q.title}</b>
        <span className="quest-bar">
          <i style={{ width: `${pct}%` }} />
        </span>
        <small>
          {Math.min(q.progress, q.goal)}/{q.goal} · thưởng {q.xu} 🪙 {q.xp} XP
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
        {msg && <span className="checkin-msg">{msg}</span>}
      </div>
    </div>
  );
}

/* =========================================================
   Hàng xóm
   ========================================================= */

function NeighborsPanel({
  onVisit,
  visiting,
}: {
  onVisit: (id: number) => void;
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
              b.stealable - a.stealable ||
              b.needs_help - a.needs_help ||
              b.xp - a.xp,
          ),
        ),
      )
      .catch((e) => setErr(errMsg(e)));
  }, [api]);
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
      <p className="farm-foot">
        Ghé vườn hàng xóm để tưới giúp, bắt sâu… hoặc trộm rau khi họ quên hái
        🫣
      </p>
      <ul className="neighbor-list">
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
                  Cấp {n.level}
                  {n.dog && " · 🐕 có chó"}
                </small>
              </span>
              <span className="neighbor-badges">
                {n.stealable > 0 && (
                  <span className="nb is-steal" title="Ô chín có thể trộm">
                    🧺 {n.stealable}
                  </span>
                )}
                {n.needs_help > 0 && (
                  <span className="nb is-help" title="Ô cần tưới / bắt sâu">
                    💧 {n.needs_help}
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
   Cửa hàng
   ========================================================= */

function ShopPanel({
  farm,
  now,
  busy,
  onBuyPlot,
  onBuyDog,
}: {
  farm: FarmView;
  now: number;
  busy: boolean;
  onBuyPlot: () => void;
  onBuyDog: () => void;
}) {
  const balance = farm.wallet?.balance ?? 0;
  const np = farm.next_plot;
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
          🟫
        </span>
        <div>
          <b>Mở rộng ô đất</b>
          <small>
            Đang có {farm.plot_count}/{farm.max_plots} ô
            {np && ` · ô tiếp theo ${np.price} 🪙, cần cấp ${np.level}`}
          </small>
        </div>
        <button
          type="button"
          className="btn btn-sm btn-primary"
          disabled={!!plotBlock || busy}
          onClick={onBuyPlot}
        >
          {plotBlock ?? `Mua ${np?.price} 🪙`}
        </button>
      </div>
      <div className="shop-card">
        <span className="shop-icon" aria-hidden>
          🐕
        </span>
        <div>
          <b>Chó giữ nhà</b>
          <small>
            Đuổi kẻ trộm 50% số lần, bắt nộp phạt 10 🪙 cho bạn.
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
          {dogLeft > 0 ? "Thêm 24h · 150 🪙" : "Mua · 150 🪙"}
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
                  <span aria-hidden>{c.emoji}</span> {c.name}
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
   Nhật ký & xếp hạng
   ========================================================= */

function logText(l: FarmLog, crops: Crop[]): string {
  const who = displayName(l.actor);
  const crop = crops.find((c) => c.id === l.crop);
  const name = crop ? crop.name.toLowerCase() : "cây";
  switch (l.action) {
    case "water":
      return `💧 ${who} tưới ${name} giúp bạn`;
    case "bug":
      return `🐛 ${who} bắt sâu cho ${name} của bạn`;
    case "steal":
      return `🫣 ${who} trộm ${l.xu ?? 0} xu ${name} của bạn`;
    case "dog":
      return `🐕 Chó đuổi ${who} chạy mất dép, bạn được ${l.xu ?? 10} xu`;
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
            {logText(l, farm.crops)}
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
            {i < 3 ? ["🥇", "🥈", "🥉"][i] : i + 1}
          </span>
          <button
            type="button"
            className="farm-top-user"
            onClick={() => r.user.id !== meId && onVisit(r.user.id)}
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
        🌾
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
