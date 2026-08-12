import {NavLink, Outlet} from "react-router-dom";

import {RequireAuth} from "@/auth/RequireAuth";
import {SignInButton} from "@/auth/SignInButton";
import {MyCertificates} from "@/features/account/MyCertificates";
import {MyComments} from "@/features/account/MyComments";
import {MyEnrollments} from "@/features/account/MyEnrollments";
import {MyOrders} from "@/features/account/MyOrders";

const TABS = [
    ["/account/enrollments", "学习中"],
    ["/account/orders", "订单"],
    ["/account/certificates", "证书"],
    ["/account/comments", "我的评论"],
] as const;

function AccountLayout() {
    return (
        <RequireAuth fallback={<div className="auth-gate panel"><span className="eyebrow">个人工作区</span><h1>请先登录以打开账户中心。</h1><p>你的课程、回执、凭据与评论都在这里。</p><SignInButton className="btn btn--primary">登录</SignInButton></div>}>
            <div className="page-stack">
                <div className="section-heading">
                    <div>
                        <span className="eyebrow">学生工作区</span>
                        <h1>学习进度，井然有序。</h1>
                        <p>在正在学习的课程、购买记录与已认证成就之间自由切换，不丢失上下文。</p>
                    </div>
                </div>
                <nav className="subnav" aria-label="账户分页">
                    {TABS.map(([to, label]) => <NavLink key={to} to={to} className={({isActive}) => `subnav__link${isActive ? " is-active" : ""}`}>{label}</NavLink>)}
                </nav>
                <Outlet />
            </div>
        </RequireAuth>
    );
}

export const AccountOrdersPage = MyOrders;
export const AccountEnrollmentsPage = MyEnrollments;
export const AccountCertificatesPage = MyCertificates;
export const AccountCommentsPage = MyComments;

export default AccountLayout;
