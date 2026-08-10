/**
 * 主页面布局（anchor-based，无 router 库）。
 *
 *   TopNav (sticky)                          ← 顶部导航 + 钱包连接
 *     ↓
//   Hero                                      ← 品牌主视觉
//     ↓
//   #catalog  → CourseCatalog                 ← 公开课程
//     ↓
//   #swap     → SwapCard                      ← YD ↔ USDC 兑换
//     ↓
//   #account  → AccountCenter
//                 ├ MyOrders                  ← 订单历史（buyer）
//                 ├ MyEnrollments             ← 已报名课程
//                 ├ MyCertificates            ← 完课证书
//                 └ MyComments                ← 我的评论
//     ↓
//   #studio   → CourseEditor                  ← 老师工作台（COURSE_CREATE）
//     ↓
//   #admin    → UsersPage + ChainStatusPanel + DLQ
//               （各 page 自带 AdminLayout 包装，按 currentPath 高亮侧栏）
//     ↓
//   Footer                                    ← 多列导航 + 状态条
 *
 * 设计原则：
 *   - 当前项目没有 react-router；所有导航走 hash anchor + 滚动。
 *   - 每个 admin 子组件（UsersPage / ChainStatusPanel）自带 AdminLayout；
 *     这里只决定"是否挂载"，由 RequirePermission 守门。
 *   - 钱包连接 + 链切换统一走 TopNav.WalletChip；本组件不重复展示。
 */
import {TopNav} from "./components/TopNav";
import {Hero} from "./components/Hero";
import {Footer} from "./components/Footer";
import {CourseCatalog} from "./features/catalog/CourseCatalog";
import {CourseEditor} from "./features/teacher/CourseEditor";
import {SwapCard} from "./features/swap/SwapCard";
import {MyOrders} from "./features/account/MyOrders";
import {MyEnrollments} from "./features/account/MyEnrollments";
import {MyCertificates} from "./features/account/MyCertificates";
import {MyComments} from "./features/account/MyComments";
import {UsersPage} from "./features/admin/users/UsersPage";
import {ChainStatusPanel} from "./features/admin/chain/ChainStatusPanel";
import {RequirePermission} from "./auth/RequirePermission";
import {useSession} from "./auth/SessionContext";

function AccountCenter() {
    const {profile} = useSession();
    if (!profile) {
        return (
            <div className="empty-state panel" aria-label="Sign in to view your account">
                <span>◇</span>
                <h3>Sign in to see your account</h3>
                <p>Sign in with Privy to view orders, enrollments, certificates and comments.</p>
            </div>
        );
    }
    return (
        <div className="account-grid">
            <section className="panel" aria-label="My orders">
                <div className="section-heading"><div><span className="eyebrow">Activity</span><h3>My orders</h3><p>Pending and confirmed Sepolia transactions.</p></div></div>
                <MyOrders />
            </section>
            <section className="panel" aria-label="My enrollments">
                <div className="section-heading"><div><span className="eyebrow">Learning</span><h3>My enrollments</h3><p>Courses you've claimed onchain. Resume from where you left off.</p></div></div>
                <MyEnrollments />
            </section>
            <section className="panel" aria-label="My certificates">
                <div className="section-heading"><div><span className="eyebrow">Credentials</span><h3>My certificates</h3><p>Onchain certificates issued after course completion.</p></div></div>
                <MyCertificates />
            </section>
            <section className="panel" aria-label="My comments">
                <div className="section-heading"><div><span className="eyebrow">Activity</span><h3>My comments</h3><p>Your reviews across all enrolled courses (including pending moderation).</p></div></div>
                <MyComments />
            </section>
        </div>
    );
}

export function App() {
    return (
        <>
            <TopNav />

            <main className="container">
                <Hero />

                {/* 公开课程 — 所有访客可见 */}
                <CourseCatalog />

                {/* 兑换卡 — 任何人可用（需连接钱包） */}
                <section id="swap" className="panel" aria-labelledby="swap-title">
                    <div className="section-heading">
                        <div>
                            <span className="eyebrow">DeFi desk</span>
                            <h2 id="swap-title">Swap</h2>
                            <p>Swap YD ↔ USDC on Sepolia via Uniswap V3 fork. Slippage ≤ 10%.</p>
                        </div>
                    </div>
                    <SwapCard />
                </section>

                {/* 账户中心 — 登录后才看到内容 */}
                <section id="account" className="account-section" aria-labelledby="account-title">
                    <div className="section-heading">
                        <div>
                            <span className="eyebrow">Your account</span>
                            <h2 id="account-title">Account center</h2>
                            <p>Orders, enrollments, certificates and reviews — all in one place.</p>
                        </div>
                    </div>
                    <AccountCenter />
                </section>

                {/* 老师工作台 — 需 COURSE_CREATE */}
                <RequirePermission code="COURSE_CREATE">
                    <section id="studio" className="studio-section" aria-labelledby="studio-title">
                        <CourseEditor />
                    </section>
                </RequirePermission>

                {/* Admin — 需 SYSTEM_ADMIN；每个子组件自带 AdminLayout */}
                <RequirePermission code="SYSTEM_ADMIN">
                    <section id="admin" className="admin-section" aria-labelledby="admin-title">
                        <div className="section-heading">
                            <div>
                                <span className="eyebrow">Operations</span>
                                <h2 id="admin-title">Admin console</h2>
                                <p>Sensitive operations — confirm intent before each action.</p>
                            </div>
                        </div>
                        <div className="admin-stack">
                            <div id="admin-users"><UsersPage /></div>
                            <div id="admin-chain"><ChainStatusPanel /></div>
                            <section className="panel" id="admin-dlq" aria-label="Dead-letter queue">
                                <div className="section-heading"><div><span className="eyebrow">Operations</span><h3>Dead-letter queue</h3><p>Events that exhausted retries; replay or discard after triage.</p></div></div>
                                <p className="muted">See <code>/admin/dlq</code> in the worker API for raw rows. UI table placeholder — wired into the worker API in F03-T17.</p>
                            </section>
                        </div>
                    </section>
                </RequirePermission>
            </main>

            <Footer />
        </>
    );
}