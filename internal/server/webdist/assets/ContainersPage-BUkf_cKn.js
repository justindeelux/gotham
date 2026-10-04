import{A as Ue}from"./Alert-BoINOGKL.js";import{d as Q,av as ie,aw as fe,o as m,c as P,n as x,ax as ye,Y as _,ad as L,ac as V,_ as N,ay as we,Z,al as ce,az as je,s as S,P as Ce,aA as ue,$ as Se,aB as Xe,aC as Ye,K as $e,O as Ve,aD as qe,z as B,aE as Qe,S as G,aF as Ke,aG as Ge,aH as Je,V as u,aI as te,W as F,ak as H,aJ as j,aK as Ze,X as ke,aL as et,aM as tt,aN as rt,am as nt,aO as ot,aP as X,aQ as at,aR as st,aS as he,a as T,F as me,aT as it,aU as lt,u as i,w as $,e as z,B as le,a9 as dt,E as ne,k as I,t as q,aa as pe,g as ct,I as ut,aq as ft,j as ht,m as oe,ab as ae,D as mt,h as pt}from"./index-DR-rq6Kt.js";import{f as ge}from"./format-length-JZ8DIHiZ.js";import{u as ve,I as gt}from"./Input-DQOdvK6o.js";import{S as J}from"./Space-CbyGLfBS.js";import{t as de}from"./text-CaYhXTor.js";import{E as vt}from"./Empty-Bbi8r_pE.js";import{T as bt}from"./Tag-B1nnXJ_F.js";import{D as yt}from"./DataTable-BjLQlZDP.js";import{N as wt}from"./Icon-DYvY_FFm.js";import{G as Ct}from"./GothamIcon-CaLBR3fG.js";import{_ as ze}from"./_plugin-vue_export-helper-DlAUqK2U.js";import{s as St,a as $t,r as kt,d as be,l as zt,L as xt}from"./LogViewer-Cie_PYt8.js";import{u as Bt}from"./use-message-B6hNPmFo.js";import{u as Et}from"./index-B9VNWVIO.js";import"./use-compitable-CCZh4Jxj.js";import"./Pagination-BtsnFuyV.js";import"./Popover-D5INop7P.js";import"./create-ref-setter-C4J8sofl.js";import"./Select-CKp9CW33.js";import"./create-DLefnRiD.js";import"./CheckboxGroup-DYn98o7G.js";import"./Tooltip-wSF9F48M.js";import"./RadioGroup-zDcaGzzh.js";import"./Dropdown-BPQlJAwh.js";import"./ChevronRight-BVl5RBTe.js";const Rt=["onMouseenter","onMouseleave","onMousedown"],Ft={key:1,role:"none"};var _t=Q({name:"NDrawerContent",inheritAttrs:!1,props:{blockScroll:Boolean,show:{type:Boolean,default:void 0},displayDirective:{type:String,required:!0},placement:{type:String,required:!0},contentClass:String,contentStyle:[Object,String],nativeScrollbar:{type:Boolean,required:!0},scrollbarProps:Object,trapFocus:{type:Boolean,default:!0},autoFocus:{type:Boolean,default:!0},showMask:{type:[Boolean,String],required:!0},maxWidth:Number,maxHeight:Number,minWidth:Number,minHeight:Number,resizable:Boolean,onClickoutside:Function,onAfterLeave:Function,onAfterEnter:Function,onEsc:Function},setup(e){const n=S(!!e.show),r=S(null),g=Ce(ue);let p=0,y="",l=null;const f=S(!1),s=S(!1),v=B(()=>e.placement==="top"||e.placement==="bottom"),{mergedClsPrefixRef:E,mergedRtlRef:R}=Se(e),M=Xe("Drawer",R,E),C=o,t=d=>{s.value=!0,p=v.value?d.clientY:d.clientX,y=document.body.style.cursor,document.body.style.cursor=v.value?"ns-resize":"ew-resize",document.body.addEventListener("mousemove",W),document.body.addEventListener("mouseleave",C),document.body.addEventListener("mouseup",o)},b=()=>{l!==null&&(window.clearTimeout(l),l=null),s.value?f.value=!0:l=window.setTimeout(()=>{f.value=!0},300)},k=()=>{l!==null&&(window.clearTimeout(l),l=null),f.value=!1},{doUpdateHeight:A,doUpdateWidth:U}=g,D=d=>{const{maxWidth:a}=e;if(a&&d>a)return a;const{minWidth:c}=e;return c&&d<c?c:d},O=d=>{const{maxHeight:a}=e;if(a&&d>a)return a;const{minHeight:c}=e;return c&&d<c?c:d};function W(d){if(s.value)if(v.value){let a=r.value?.offsetHeight||0;const c=p-d.clientY;a+=e.placement==="bottom"?c:-c,a=O(a),A(a),p=d.clientY}else{let a=r.value?.offsetWidth||0;const c=p-d.clientX;a+=e.placement==="right"?c:-c,a=D(a),U(a),p=d.clientX}}function o(){s.value&&(p=0,s.value=!1,document.body.style.cursor=y,document.body.removeEventListener("mousemove",W),document.body.removeEventListener("mouseup",o),document.body.removeEventListener("mouseleave",C))}Ye(()=>{e.show&&(n.value=!0)}),$e(()=>e.show,d=>{d||o()}),Ve(()=>{o()});const h=B(()=>{const{show:d}=e,a=[[fe,d]];return e.showMask||a.push([Qe,e.onClickoutside,void 0,{capture:!0}]),a});function w(){n.value=!1,e.onAfterLeave?.()}return qe(B(()=>e.blockScroll&&n.value)),G(Ke,r),G(Ge,null),G(Je,null),{bodyRef:r,rtlEnabled:M,mergedClsPrefix:g.mergedClsPrefixRef,isMounted:g.isMountedRef,mergedTheme:g.mergedThemeRef,displayed:n,transitionName:B(()=>({right:"slide-in-from-right-transition",left:"slide-in-from-left-transition",top:"slide-in-from-top-transition",bottom:"slide-in-from-bottom-transition"})[e.placement]),handleAfterLeave:w,bodyDirectives:h,handleMousedownResizeTrigger:t,handleMouseenterResizeTrigger:b,handleMouseleaveResizeTrigger:k,isDragging:s,isHoverOnResizeTrigger:f}},render(){const{$slots:e,mergedClsPrefix:n}=this;return this.displayDirective==="show"||this.displayed||this.show?ie((m(),P("div",Ft,[(m(),x(je,{disabled:!this.showMask||!this.trapFocus,active:this.show,autoFocus:this.autoFocus,onEsc:this.onEsc},{default:()=>(m(),x(ye,{name:this.transitionName,appear:this.isMounted,onAfterEnter:this.onAfterEnter,onAfterLeave:this.handleAfterLeave},{default:()=>ie(_("div",Z(this.$attrs,{role:"dialog",ref:"bodyRef","aria-modal":"true",class:[`${n}-drawer`,this.rtlEnabled&&`${n}-drawer--rtl`,`${n}-drawer--${this.placement}-placement`,this.isDragging&&`${n}-drawer--unselectable`,this.nativeScrollbar&&`${n}-drawer--native-scrollbar`]}),[this.resizable?(m(),P("div",{key:2,class:L([`${n}-drawer__resize-trigger`,(this.isDragging||this.isHoverOnResizeTrigger)&&`${n}-drawer__resize-trigger--hover`]),onMouseenter:this.handleMouseenterResizeTrigger,onMouseleave:this.handleMouseleaveResizeTrigger,onMousedown:this.handleMousedownResizeTrigger},null,42,Rt)):null,this.nativeScrollbar?(m(),P("div",{key:3,class:L([`${n}-drawer-content-wrapper`,this.contentClass]),style:V(this.contentStyle),role:"none"},[N(()=>e.default?.())],6)):(m(),x(we,Z({key:4},this.scrollbarProps,{contentStyle:this.contentStyle,contentClass:[`${n}-drawer-content-wrapper`,this.contentClass],theme:this.mergedTheme.peers.Scrollbar,themeOverrides:this.mergedTheme.peerOverrides.Scrollbar}),ce(e),1040,["contentStyle","contentClass","theme","themeOverrides"]))]),this.bodyDirectives)},1032,["name","appear","onAfterEnter","onAfterLeave"]))},1032,["disabled","active","autoFocus","onEsc"]))])),[[fe,this.displayDirective==="if"||this.displayed||this.show]]):null}});const{cubicBezierEaseIn:Tt,cubicBezierEaseOut:Pt}=te;function Mt({duration:e="0.3s",leaveDuration:n="0.2s",name:r="slide-in-from-bottom"}={}){return[u(`&.${r}-transition-leave-active`,{transition:`transform ${n} ${Tt}`}),u(`&.${r}-transition-enter-active`,{transition:`transform ${e} ${Pt}`}),u(`&.${r}-transition-enter-to`,{transform:"translateY(0)"}),u(`&.${r}-transition-enter-from`,{transform:"translateY(100%)"}),u(`&.${r}-transition-leave-from`,{transform:"translateY(0)"}),u(`&.${r}-transition-leave-to`,{transform:"translateY(100%)"})]}const{cubicBezierEaseIn:Ot,cubicBezierEaseOut:Lt}=te;function At({duration:e="0.3s",leaveDuration:n="0.2s",name:r="slide-in-from-left"}={}){return[u(`&.${r}-transition-leave-active`,{transition:`transform ${n} ${Ot}`}),u(`&.${r}-transition-enter-active`,{transition:`transform ${e} ${Lt}`}),u(`&.${r}-transition-enter-to`,{transform:"translateX(0)"}),u(`&.${r}-transition-enter-from`,{transform:"translateX(-100%)"}),u(`&.${r}-transition-leave-from`,{transform:"translateX(0)"}),u(`&.${r}-transition-leave-to`,{transform:"translateX(-100%)"})]}const{cubicBezierEaseIn:Dt,cubicBezierEaseOut:Ht}=te;function Nt({duration:e="0.3s",leaveDuration:n="0.2s",name:r="slide-in-from-right"}={}){return[u(`&.${r}-transition-leave-active`,{transition:`transform ${n} ${Dt}`}),u(`&.${r}-transition-enter-active`,{transition:`transform ${e} ${Ht}`}),u(`&.${r}-transition-enter-to`,{transform:"translateX(0)"}),u(`&.${r}-transition-enter-from`,{transform:"translateX(100%)"}),u(`&.${r}-transition-leave-from`,{transform:"translateX(0)"}),u(`&.${r}-transition-leave-to`,{transform:"translateX(100%)"})]}const{cubicBezierEaseIn:Wt,cubicBezierEaseOut:It}=te;function Ut({duration:e="0.3s",leaveDuration:n="0.2s",name:r="slide-in-from-top"}={}){return[u(`&.${r}-transition-leave-active`,{transition:`transform ${n} ${Wt}`}),u(`&.${r}-transition-enter-active`,{transition:`transform ${e} ${It}`}),u(`&.${r}-transition-enter-to`,{transform:"translateY(0)"}),u(`&.${r}-transition-enter-from`,{transform:"translateY(-100%)"}),u(`&.${r}-transition-leave-from`,{transform:"translateY(0)"}),u(`&.${r}-transition-leave-to`,{transform:"translateY(-100%)"})]}var jt=u([F("drawer",`
 word-break: break-word;
 line-height: var(--n-line-height);
 position: absolute;
 pointer-events: all;
 box-shadow: var(--n-box-shadow);
 transition:
 background-color .3s var(--n-bezier),
 color .3s var(--n-bezier);
 background-color: var(--n-color);
 color: var(--n-text-color);
 box-sizing: border-box;
 `,[Nt(),At(),Ut(),Mt(),H("unselectable",`
 user-select: none; 
 -webkit-user-select: none;
 `),H("native-scrollbar",[F("drawer-content-wrapper",`
 overflow: auto;
 height: 100%;
 `)]),j("resize-trigger",`
 position: absolute;
 background-color: #0000;
 transition: background-color .3s var(--n-bezier);
 `,[H("hover",`
 background-color: var(--n-resize-trigger-color-hover);
 `)]),F("drawer-content-wrapper",`
 box-sizing: border-box;
 `),F("drawer-content",`
 height: 100%;
 display: flex;
 flex-direction: column;
 `,[H("native-scrollbar",[F("drawer-body-content-wrapper",`
 height: 100%;
 overflow: auto;
 `)]),F("drawer-body",`
 flex: 1 0 0;
 overflow: hidden;
 `),F("drawer-body-content-wrapper",`
 box-sizing: border-box;
 padding: var(--n-body-padding);
 `),F("drawer-header",`
 font-weight: var(--n-title-font-weight);
 line-height: 1;
 font-size: var(--n-title-font-size);
 color: var(--n-title-text-color);
 padding: var(--n-header-padding);
 transition: border .3s var(--n-bezier);
 border-bottom: 1px solid var(--n-divider-color);
 border-bottom: var(--n-header-border-bottom);
 display: flex;
 justify-content: space-between;
 align-items: center;
 `,[j("main",`
 flex: 1;
 `),j("close",`
 margin-left: 6px;
 transition:
 background-color .3s var(--n-bezier),
 color .3s var(--n-bezier);
 `)]),F("drawer-footer",`
 display: flex;
 justify-content: flex-end;
 border-top: var(--n-footer-border-top);
 transition: border .3s var(--n-bezier);
 padding: var(--n-footer-padding);
 `)]),H("right-placement",`
 top: 0;
 bottom: 0;
 right: 0;
 border-top-left-radius: var(--n-border-radius);
 border-bottom-left-radius: var(--n-border-radius);
 `,[j("resize-trigger",`
 width: 3px;
 height: 100%;
 top: 0;
 left: 0;
 transform: translateX(-1.5px);
 cursor: ew-resize;
 `)]),H("left-placement",`
 top: 0;
 bottom: 0;
 left: 0;
 border-top-right-radius: var(--n-border-radius);
 border-bottom-right-radius: var(--n-border-radius);
 `,[j("resize-trigger",`
 width: 3px;
 height: 100%;
 top: 0;
 right: 0;
 transform: translateX(1.5px);
 cursor: ew-resize;
 `)]),H("top-placement",`
 top: 0;
 left: 0;
 right: 0;
 border-bottom-left-radius: var(--n-border-radius);
 border-bottom-right-radius: var(--n-border-radius);
 `,[j("resize-trigger",`
 width: 100%;
 height: 3px;
 bottom: 0;
 left: 0;
 transform: translateY(1.5px);
 cursor: ns-resize;
 `)]),H("bottom-placement",`
 left: 0;
 bottom: 0;
 right: 0;
 border-top-left-radius: var(--n-border-radius);
 border-top-right-radius: var(--n-border-radius);
 `,[j("resize-trigger",`
 width: 100%;
 height: 3px;
 top: 0;
 left: 0;
 transform: translateY(-1.5px);
 cursor: ns-resize;
 `)])]),u("body",[u(">",[F("drawer-container",`
 position: fixed;
 `)])]),F("drawer-container",`
 position: relative;
 position: absolute;
 left: 0;
 right: 0;
 top: 0;
 bottom: 0;
 pointer-events: none;
 `,[u("> *",`
 pointer-events: all;
 `)]),F("drawer-mask",`
 background-color: rgba(0, 0, 0, .3);
 position: absolute;
 left: 0;
 right: 0;
 top: 0;
 bottom: 0;
 `,[H("invisible",`
 background-color: rgba(0, 0, 0, 0)
 `),Ze({enterDuration:"0.2s",leaveDuration:"0.2s",enterCubicBezier:"var(--n-bezier-in)",leaveCubicBezier:"var(--n-bezier-out)"})])]);const Xt=["onClick"],Yt={...ke.props,show:Boolean,width:[Number,String],height:[Number,String],placement:{type:String,default:"right"},maskClosable:{type:Boolean,default:!0},showMask:{type:[Boolean,String],default:!0},to:[String,Object],displayDirective:{type:String,default:"if"},nativeScrollbar:{type:Boolean,default:!0},zIndex:Number,onMaskClick:Function,scrollbarProps:Object,contentClass:String,contentStyle:[Object,String],trapFocus:{type:Boolean,default:!0},onEsc:Function,autoFocus:{type:Boolean,default:!0},closeOnEsc:{type:Boolean,default:!0},blockScroll:{type:Boolean,default:!0},maxWidth:Number,maxHeight:Number,minWidth:Number,minHeight:Number,resizable:Boolean,defaultWidth:{type:[Number,String],default:251},defaultHeight:{type:[Number,String],default:251},onUpdateWidth:[Function,Array],onUpdateHeight:[Function,Array],"onUpdate:width":[Function,Array],"onUpdate:height":[Function,Array],"onUpdate:show":[Function,Array],onUpdateShow:[Function,Array],onAfterEnter:Function,onAfterLeave:Function,drawerStyle:[String,Object],drawerClass:String,target:null,onShow:Function,onHide:Function};var Vt=Q({name:"Drawer",inheritAttrs:!1,props:Yt,setup(e){const{mergedClsPrefixRef:n,namespaceRef:r,inlineThemeDisabled:g}=Se(e),p=tt(),y=ke("Drawer","-drawer",jt,at,e,n),l=S(e.defaultWidth),f=S(e.defaultHeight),s=ve(he(e,"width"),l),v=ve(he(e,"height"),f),E=B(()=>{const{placement:o}=e;return o==="top"||o==="bottom"?"":ge(s.value)}),R=B(()=>{const{placement:o}=e;return o==="left"||o==="right"?"":ge(v.value)}),M=o=>{const{onUpdateWidth:h,"onUpdate:width":w}=e;h&&X(h,o),w&&X(w,o),l.value=o},C=o=>{const{onUpdateHeight:h,"onUpdate:width":w}=e;h&&X(h,o),w&&X(w,o),f.value=o},t=B(()=>[{width:E.value,height:R.value},e.drawerStyle||""]);function b(o){const{onMaskClick:h,maskClosable:w}=e;w&&D(!1),h&&h(o)}function k(o){b(o)}const A=rt();function U(o){e.onEsc?.(),e.show&&e.closeOnEsc&&ot(o)&&(A.value||D(!1))}function D(o){const{onHide:h,onUpdateShow:w,"onUpdate:show":d}=e;w&&X(w,o),d&&X(d,o),h&&!o&&X(h,o)}G(ue,{isMountedRef:p,mergedThemeRef:y,mergedClsPrefixRef:n,doUpdateShow:D,doUpdateHeight:C,doUpdateWidth:M});const O=B(()=>{const{common:{cubicBezierEaseInOut:o,cubicBezierEaseIn:h,cubicBezierEaseOut:w},self:{color:d,textColor:a,boxShadow:c,lineHeight:re,headerPadding:K,footerPadding:Y,borderRadius:Be,bodyPadding:Ee,titleFontSize:Re,titleTextColor:Fe,titleFontWeight:_e,headerBorderBottom:Te,footerBorderTop:Pe,closeIconColor:Me,closeIconColorHover:Oe,closeIconColorPressed:Le,closeColorHover:Ae,closeColorPressed:De,closeIconSize:He,closeSize:Ne,closeBorderRadius:We,resizableTriggerColorHover:Ie}}=y.value;return{"--n-line-height":re,"--n-color":d,"--n-border-radius":Be,"--n-text-color":a,"--n-box-shadow":c,"--n-bezier":o,"--n-bezier-out":w,"--n-bezier-in":h,"--n-header-padding":K,"--n-body-padding":Ee,"--n-footer-padding":Y,"--n-title-text-color":Fe,"--n-title-font-size":Re,"--n-title-font-weight":_e,"--n-header-border-bottom":Te,"--n-footer-border-top":Pe,"--n-close-icon-color":Me,"--n-close-icon-color-hover":Oe,"--n-close-icon-color-pressed":Le,"--n-close-size":Ne,"--n-close-color-hover":Ae,"--n-close-color-pressed":De,"--n-close-icon-size":He,"--n-close-border-radius":We,"--n-resize-trigger-color-hover":Ie}}),W=g?nt("drawer",void 0,O,e):void 0;return{mergedClsPrefix:n,namespace:r,mergedBodyStyle:t,handleOutsideClick:k,handleMaskClick:b,handleEsc:U,mergedTheme:y,cssVars:g?void 0:O,themeClass:W?.themeClass,onRender:W?.onRender,isMounted:p}},render(){const{mergedClsPrefix:e}=this;return m(),x(et,{to:this.to,show:this.show},{default:()=>(this.onRender?.(),ie((m(),P("div",{class:L([`${e}-drawer-container`,this.namespace,this.themeClass]),style:V(this.cssVars),role:"none"},[this.showMask?(m(),x(ye,{key:0,name:"fade-in-transition",appear:this.isMounted},{default:()=>this.show?(m(),P("div",{key:1,"aria-hidden":!0,class:L([`${e}-drawer-mask`,this.showMask==="transparent"&&`${e}-drawer-mask--invisible`]),onClick:this.handleMaskClick},null,10,Xt)):null},1032,["appear"])):N(()=>null),(m(),x(_t,Z(this.$attrs,{class:[this.drawerClass,this.$attrs.class],style:[this.mergedBodyStyle,this.$attrs.style],blockScroll:this.blockScroll,contentStyle:this.contentStyle,contentClass:this.contentClass,placement:this.placement,scrollbarProps:this.scrollbarProps,show:this.show,displayDirective:this.displayDirective,nativeScrollbar:this.nativeScrollbar,onAfterEnter:this.onAfterEnter,onAfterLeave:this.onAfterLeave,trapFocus:this.trapFocus,autoFocus:this.autoFocus,resizable:this.resizable,maxHeight:this.maxHeight,minHeight:this.minHeight,maxWidth:this.maxWidth,minWidth:this.minWidth,showMask:this.showMask,onEsc:this.handleEsc,onClickoutside:this.handleOutsideClick}),ce(this.$slots),1040,["class","style","blockScroll","contentStyle","contentClass","placement","scrollbarProps","show","displayDirective","nativeScrollbar","onAfterEnter","onAfterLeave","trapFocus","autoFocus","resizable","maxHeight","minHeight","maxWidth","minWidth","showMask","onEsc","onClickoutside"]))],6)),[[st,{zIndex:this.zIndex,enabled:this.show}]]))},1032,["to","show"])}});const qt={title:String,headerClass:String,headerStyle:[Object,String],footerClass:String,footerStyle:[Object,String],bodyClass:String,bodyStyle:[Object,String],bodyContentClass:String,bodyContentStyle:[Object,String],nativeScrollbar:{type:Boolean,default:!0},scrollbarProps:Object,closable:Boolean};var Qt=Q({name:"DrawerContent",props:qt,slots:Object,setup(){const e=Ce(ue,null);e||lt("drawer-content","`n-drawer-content` must be placed inside `n-drawer`.");const{doUpdateShow:n}=e;function r(){n(!1)}return{handleCloseClick:r,mergedTheme:e.mergedThemeRef,mergedClsPrefix:e.mergedClsPrefixRef}},render(){const{title:e,mergedClsPrefix:n,nativeScrollbar:r,mergedTheme:g,bodyClass:p,bodyStyle:y,bodyContentClass:l,bodyContentStyle:f,headerClass:s,headerStyle:v,footerClass:E,footerStyle:R,scrollbarProps:M,closable:C,$slots:t}=this;return m(),P("div",{role:"none",class:L([`${n}-drawer-content`,r&&`${n}-drawer-content--native-scrollbar`])},[t.header||e||C?(m(),P("div",{key:0,class:L([`${n}-drawer-header`,s]),style:V(v),role:"none"},[T("div",{class:L(`${n}-drawer-header__main`),role:"heading","aria-level":"1"},[t.header!==void 0?(m(),P(me,{key:0},[N(()=>t.header())],64)):(m(),P(me,{key:1},[N(()=>e)],64))],2),N(()=>C&&(m(),x(it,{onClick:this.handleCloseClick,clsPrefix:n,class:L(`${n}-drawer-header__close`),absolute:!0},null,8,["onClick","clsPrefix","class"])))],6)):N(()=>null),r?(m(),P("div",{key:2,class:L([`${n}-drawer-body`,p]),style:V(y),role:"none"},[T("div",{class:L([`${n}-drawer-body-content-wrapper`,l]),style:V(f),role:"none"},[N(()=>t.default?.())],6)],6)):(m(),x(we,Z({key:3,themeOverrides:g.peerOverrides.Scrollbar,theme:g.peers.Scrollbar},M,{class:`${n}-drawer-body`,contentClass:[`${n}-drawer-body-content-wrapper`,l],contentStyle:f}),ce(t),1040,["themeOverrides","theme","class","contentClass","contentStyle"])),t.footer?(m(),P("div",{key:4,class:L([`${n}-drawer-footer`,E]),style:V(R),role:"none"},[N(()=>t.footer())],6)):N(()=>null)],2)}});function ee(e){return e.trim().toLowerCase()==="running"}function xe(e,n){switch(n){case"running":return ee(e.state);case"exited":return!ee(e.state);default:return!0}}function Kt(e){switch(e.trim().toLowerCase()){case"running":return"success";case"restarting":case"paused":case"created":return"warning";case"exited":case"dead":return"error";default:return"default"}}function se(e,n){return n==="all"?e.length:e.filter(r=>xe(r,n)).length}const Gt=Q({__name:"ContainersTable",props:{containers:{},loading:{type:Boolean},emptyDescription:{},isPending:{type:Function},isBusy:{type:Function}},emits:["action","openLogs"],setup(e,{emit:n}){const r=e,g=n;function p(t){const b=t.status||t.state||"unknown";return _("span",{title:b},[_(bt,{type:Kt(t.state),size:"small",round:!0},{default:()=>t.state||"unknown"})])}function y(t){return!t.ports||t.ports.length===0?_(de,{depth:3},{default:()=>"—"}):_("span",{class:"mono"},t.ports.join(", "))}function l(){return _("span",{class:"mono muted",title:"Not reported by the agent yet","aria-label":"Not reported by the agent yet"},"—")}function f(t){return _("span",{class:"mono muted"},t.status||"—")}function s(t,b,k){return _(le,{size:"small",secondary:!0,loading:r.isPending(t.id,b),disabled:r.isBusy(t.id),onClick:A=>{A.stopPropagation(),g("action",t,b,k)}},{default:()=>k})}function v(t){return _(le,{size:"small",quaternary:!0,onClick:b=>{b.stopPropagation(),g("openLogs",t)}},{default:()=>"Logs"})}function E(t){const b=[];return ee(t.state)?(b.push(s(t,"restart","Restart")),b.push(s(t,"stop","Stop"))):b.push(s(t,"start","Start")),b.push(v(t)),_(J,{size:8,align:"center",wrap:!1},{default:()=>b})}const R=[{title:"Name",key:"name",minWidth:180,ellipsis:{tooltip:!0},render:t=>_("span",{class:"mono"},t.name||t.id)},{title:"Image",key:"image",minWidth:180,ellipsis:{tooltip:!0},render:t=>_("span",{class:"mono muted"},t.image||"—")},{title:"State",key:"state",width:140,render:t=>p(t)},{title:"Ports",key:"ports",minWidth:140,render:t=>y(t)},{title:"CPU",key:"cpu",width:80,render:()=>l()},{title:"RAM",key:"ram",width:90,render:()=>l()},{title:"Uptime",key:"uptime",minWidth:140,ellipsis:{tooltip:!0},render:t=>f(t)},{title:"Actions",key:"actions",width:220,render:t=>E(t)}];function M(t){return t.id}function C(t){return{style:"cursor: pointer;",onClick:()=>g("openLogs",t)}}return(t,b)=>(m(),x(i(yt),{columns:R,data:e.containers,loading:e.loading,"row-key":M,"row-props":C,bordered:!1,"scroll-x":1200,pagination:{pageSize:10}},{empty:$(()=>[z(i(vt),{description:e.emptyDescription},null,8,["description"])]),_:1},8,["data","loading"]))}}),Jt={class:"toolbar"},Zt={class:"filters",role:"group","aria-label":"Filter containers by status"},er=["aria-pressed"],tr={class:"nav-count"},rr=["aria-pressed"],nr={class:"nav-count"},or=["aria-pressed"],ar={class:"nav-count"},sr=Q({__name:"ContainersToolbar",props:pe({activeFilter:{},filterCounts:{}},{searchQuery:{required:!0},searchQueryModifiers:{}}),emits:pe(["update:activeFilter"],["update:searchQuery"]),setup(e,{emit:n}){const r=e,g=n,p=dt(e,"searchQuery");function y(f){g("update:activeFilter",f)}function l(f){return r.activeFilter===f}return(f,s)=>(m(),P("div",Jt,[T("div",Zt,[T("button",{class:ne(["chip",{"is-active":l("all")}]),type:"button","aria-pressed":l("all"),onClick:s[0]||(s[0]=v=>y("all"))},[s[4]||(s[4]=I(" All ",-1)),T("span",tr,q(e.filterCounts.all),1)],10,er),T("button",{class:ne(["chip",{"is-active":l("running")}]),type:"button","aria-pressed":l("running"),onClick:s[1]||(s[1]=v=>y("running"))},[s[5]||(s[5]=I(" Running ",-1)),T("span",nr,q(e.filterCounts.running),1)],10,rr),T("button",{class:ne(["chip",{"is-active":l("exited")}]),type:"button","aria-pressed":l("exited"),onClick:s[2]||(s[2]=v=>y("exited"))},[s[6]||(s[6]=I(" Exited ",-1)),T("span",ar,q(e.filterCounts.exited),1)],10,or)]),z(i(gt),{value:p.value,"onUpdate:value":s[3]||(s[3]=v=>p.value=v),class:"search-input",placeholder:"Search container or image…","aria-label":"Search containers",clearable:""},{prefix:$(()=>[z(i(wt),null,{default:$(()=>[z(Ct,{name:"search"})]),_:1})]),_:1},8,["value"])]))}}),ir=ze(sr,[["__scopeId","data-v-8827e7e5"]]),lr=5e3;function dr(e){const n=Bt(),r=S([]),g=S(!1),p=S(null),y=S(""),l=S(!1),f=S({}),s=S(null),v=S(!1),E=S("all"),R=S(""),M=B(()=>{const a=R.value.trim().toLowerCase();return r.value.filter(c=>xe(c,E.value)?a===""?!0:`${c.name} ${c.image}`.toLowerCase().includes(a):!1)}),C=B(()=>({all:se(r.value,"all"),running:se(r.value,"running"),exited:se(r.value,"exited")})),t=Et("(max-width: 760px)"),b=B(()=>t.value?"94vw":720);let k=null;function A(a,c){return f.value[`${a}:${c}`]===!0}function U(a){return Object.keys(f.value).some(c=>c.startsWith(`${a}:`))}function D(a){s.value=a,v.value=!0}async function O(a){if(e.value){a&&(g.value=!0);try{r.value=await zt(e.value),p.value=null,l.value=!0}catch(c){p.value=be(c)}finally{g.value=!1}}}async function W(){if(e.value)try{const a=await ft(e.value);y.value=a.name}catch{}}async function o(a,c,re){const K=`${a.id}:${c}`;f.value={...f.value,[K]:!0};try{c==="start"?await St(e.value,a.id):c==="stop"?await $t(e.value,a.id):await kt(e.value,a.id),n.success(`${re} requested for ${a.name}`),await O(!1)}catch(Y){n.error(be(Y))}finally{const Y={...f.value};delete Y[K],f.value=Y}}function h(){k===null&&(k=setInterval(()=>{O(!1)},lr))}function w(){k!==null&&(clearInterval(k),k=null)}async function d(){l.value=!1,await Promise.all([W(),O(!0)])}return $e(()=>e.value,()=>{s.value=null,v.value=!1,d()}),ct(()=>{d(),h()}),ut(()=>{w()}),{containers:r,loading:g,error:p,serverName:y,loaded:l,activeFilter:E,searchQuery:R,filteredContainers:M,filterCounts:C,drawerWidth:b,selected:s,drawerOpen:v,isRunning:ee,isPending:A,isBusy:U,openLogs:D,fetchContainers:O,runAction:o}}const cr={class:"breadcrumb","aria-label":"Breadcrumb"},ur={class:"muted","aria-current":"page"},fr=Q({__name:"ContainersPage",setup(e){const n=pt(),r=B(()=>String(n.params.id??"")),{containers:g,loading:p,error:y,serverName:l,loaded:f,activeFilter:s,searchQuery:v,filteredContainers:E,filterCounts:R,drawerWidth:M,selected:C,drawerOpen:t,isPending:b,isBusy:k,openLogs:A,fetchContainers:U,runAction:D}=dr(r),O=B(()=>f.value?g.value.length===0?"No containers on this node.":"No containers match the current filter.":"Loading containers…");return(W,o)=>(m(),x(i(J),{vertical:"",size:16},{default:$(()=>[T("nav",cr,[z(i(ht),{to:"/servers"},{default:$(()=>[...o[5]||(o[5]=[I("Servers",-1)])]),_:1}),o[6]||(o[6]=T("span",{class:"breadcrumb__sep"},"/",-1)),T("span",ur,q(i(l)||r.value),1)]),z(i(mt),null,{header:$(()=>[z(i(J),{align:"center",justify:"space-between"},{default:$(()=>[z(i(J),{align:"center",size:10},{default:$(()=>[z(i(de),{strong:""},{default:$(()=>[...o[7]||(o[7]=[I("Containers",-1)])]),_:1}),i(l)?(m(),x(i(de),{key:0,depth:"3"},{default:$(()=>[I(q(i(l)),1)]),_:1})):oe("",!0)]),_:1}),z(i(le),{secondary:"",loading:i(p),onClick:o[0]||(o[0]=h=>i(U)(!0))},{default:$(()=>[...o[8]||(o[8]=[I(" Refresh ",-1)])]),_:1},8,["loading"])]),_:1})]),default:$(()=>[i(y)?(m(),x(i(Ue),{key:0,type:"error","show-icon":!0,style:{"margin-bottom":"12px"}},{default:$(()=>[I(q(i(y)),1)]),_:1})):oe("",!0),z(ir,{"active-filter":i(s),"onUpdate:activeFilter":o[1]||(o[1]=h=>ae(s)?s.value=h:null),"search-query":i(v),"onUpdate:searchQuery":o[2]||(o[2]=h=>ae(v)?v.value=h:null),"filter-counts":i(R)},null,8,["active-filter","search-query","filter-counts"]),z(Gt,{containers:i(E),loading:i(p),"empty-description":O.value,"is-pending":i(b),"is-busy":i(k),onAction:o[3]||(o[3]=(h,w,d)=>{i(D)(h,w,d)}),onOpenLogs:i(A)},null,8,["containers","loading","empty-description","is-pending","is-busy","onOpenLogs"])]),_:1}),z(i(Vt),{show:i(t),"onUpdate:show":o[4]||(o[4]=h=>ae(t)?t.value=h:null),width:i(M),placement:"right"},{default:$(()=>[z(i(Qt),{closable:"","native-scrollbar":!1},{default:$(()=>[i(C)?(m(),x(xt,{key:0,"server-id":r.value,"container-id":i(C).id,title:i(C).name,subtitle:i(C).image,"auto-start-stream":!0},null,8,["server-id","container-id","title","subtitle"])):oe("",!0)]),_:1})]),_:1},8,["show","width"])]),_:1}))}}),Hr=ze(fr,[["__scopeId","data-v-c09f88d2"]]);export{Hr as default};
