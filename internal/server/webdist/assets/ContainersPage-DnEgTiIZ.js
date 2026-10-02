import{E as He}from"./Empty-ffz4KaPB.js";import{T as Ie}from"./Tag-oYoHa3pi.js";import{A as De}from"./Alert-BsDiwXpA.js";import{d as oe,a6 as he,a7 as ge,o as d,c as E,m as x,a8 as Se,L as T,V as B,U as V,N as I,a9 as ke,M as re,a1 as me,aa as Ae,q as w,ab as $e,ac as ve,O as ze,ad as We,ae as Le,A as xe,a4 as Ne,af as Ue,y as M,ag as je,ah as te,ai as Xe,aj as Ye,ak as Ve,I as s,al as ne,J as z,a0 as H,am as W,an as qe,K as Be,ao as Ke,ap as Je,aq as Qe,a2 as Ge,ar as Ze,as as L,at as et,au as tt,av as be,a as G,F as pe,aw as rt,ax as ot,g as nt,D as at,w as S,u as k,e as _,k as Q,j as st,t as de,l as ce,B as ue,C as it,h as lt}from"./index-BdG6kN7m.js";import{D as dt}from"./DataTable-w--CLY1w.js";import{u as ct}from"./use-message-BZtlukmM.js";import{f as ye}from"./format-length-Dl4QTwvw.js";import{u as we}from"./use-merged-state-BLX1ZuWR.js";import{S as ee}from"./Space-P69wviH-.js";import{t as fe}from"./text-BZq6PQJ8.js";import{l as ut,d as Ce,L as ft,s as ht,a as mt,r as vt}from"./LogViewer-E5qYpIbB.js";import{g as gt}from"./servers-Dhcn6ZEQ.js";import{u as bt}from"./useMediaQuery-DifuC5gi.js";import{_ as pt}from"./_plugin-vue_export-helper-DlAUqK2U.js";import"./Pagination-EmaFEv94.js";import"./Popover-8k-a4Z4_.js";import"./Input-C8D6tGMP.js";import"./create-ref-setter-C4J8sofl.js";import"./Select-D4tM7euH.js";import"./create-DLefnRiD.js";import"./CheckboxGroup-D-WIrcdc.js";import"./Tooltip-DAJRgNY9.js";import"./RadioGroup-DKspBGpc.js";import"./Dropdown-CeHqCFN-.js";import"./ChevronRight-DkPkLWdA.js";import"./Icon-DO0upaaU.js";const yt=["onMouseenter","onMouseleave","onMousedown"],wt={key:1,role:"none"};var Ct=oe({name:"NDrawerContent",inheritAttrs:!1,props:{blockScroll:Boolean,show:{type:Boolean,default:void 0},displayDirective:{type:String,required:!0},placement:{type:String,required:!0},contentClass:String,contentStyle:[Object,String],nativeScrollbar:{type:Boolean,required:!0},scrollbarProps:Object,trapFocus:{type:Boolean,default:!0},autoFocus:{type:Boolean,default:!0},showMask:{type:[Boolean,String],required:!0},maxWidth:Number,maxHeight:Number,minWidth:Number,minHeight:Number,resizable:Boolean,onClickoutside:Function,onAfterLeave:Function,onAfterEnter:Function,onEsc:Function},setup(e){const r=w(!!e.show),o=w(null),c=$e(ve);let b=0,C="",u=null;const g=w(!1),p=w(!1),y=M(()=>e.placement==="top"||e.placement==="bottom"),{mergedClsPrefixRef:$,mergedRtlRef:R}=ze(e),D=We("Drawer",R,$),P=a,h=i=>{p.value=!0,b=y.value?i.clientY:i.clientX,C=document.body.style.cursor,document.body.style.cursor=y.value?"ns-resize":"ew-resize",document.body.addEventListener("mousemove",F),document.body.addEventListener("mouseleave",P),document.body.addEventListener("mouseup",a)},N=()=>{u!==null&&(window.clearTimeout(u),u=null),p.value?g.value=!0:u=window.setTimeout(()=>{g.value=!0},300)},q=()=>{u!==null&&(window.clearTimeout(u),u=null),g.value=!1},{doUpdateHeight:K,doUpdateWidth:J}=c,A=i=>{const{maxWidth:l}=e;if(l&&i>l)return l;const{minWidth:f}=e;return f&&i<f?f:i},U=i=>{const{maxHeight:l}=e;if(l&&i>l)return l;const{minHeight:f}=e;return f&&i<f?f:i};function F(i){if(p.value)if(y.value){let l=o.value?.offsetHeight||0;const f=b-i.clientY;l+=e.placement==="bottom"?f:-f,l=U(l),K(l),b=i.clientY}else{let l=o.value?.offsetWidth||0;const f=b-i.clientX;l+=e.placement==="right"?f:-f,l=A(l),J(l),b=i.clientX}}function a(){p.value&&(b=0,p.value=!1,document.body.style.cursor=C,document.body.removeEventListener("mousemove",F),document.body.removeEventListener("mouseup",a),document.body.removeEventListener("mouseleave",P))}Le(()=>{e.show&&(r.value=!0)}),xe(()=>e.show,i=>{i||a()}),Ne(()=>{a()});const m=M(()=>{const{show:i}=e,l=[[ge,i]];return e.showMask||l.push([je,e.onClickoutside,void 0,{capture:!0}]),l});function v(){r.value=!1,e.onAfterLeave?.()}return Ue(M(()=>e.blockScroll&&r.value)),te(Xe,o),te(Ye,null),te(Ve,null),{bodyRef:o,rtlEnabled:D,mergedClsPrefix:c.mergedClsPrefixRef,isMounted:c.isMountedRef,mergedTheme:c.mergedThemeRef,displayed:r,transitionName:M(()=>({right:"slide-in-from-right-transition",left:"slide-in-from-left-transition",top:"slide-in-from-top-transition",bottom:"slide-in-from-bottom-transition"})[e.placement]),handleAfterLeave:v,bodyDirectives:m,handleMousedownResizeTrigger:h,handleMouseenterResizeTrigger:N,handleMouseleaveResizeTrigger:q,isDragging:p,isHoverOnResizeTrigger:g}},render(){const{$slots:e,mergedClsPrefix:r}=this;return this.displayDirective==="show"||this.displayed||this.show?he((d(),E("div",wt,[(d(),x(Ae,{disabled:!this.showMask||!this.trapFocus,active:this.show,autoFocus:this.autoFocus,onEsc:this.onEsc},{default:()=>(d(),x(Se,{name:this.transitionName,appear:this.isMounted,onAfterEnter:this.onAfterEnter,onAfterLeave:this.handleAfterLeave},{default:()=>he(T("div",re(this.$attrs,{role:"dialog",ref:"bodyRef","aria-modal":"true",class:[`${r}-drawer`,this.rtlEnabled&&`${r}-drawer--rtl`,`${r}-drawer--${this.placement}-placement`,this.isDragging&&`${r}-drawer--unselectable`,this.nativeScrollbar&&`${r}-drawer--native-scrollbar`]}),[this.resizable?(d(),E("div",{key:2,class:B([`${r}-drawer__resize-trigger`,(this.isDragging||this.isHoverOnResizeTrigger)&&`${r}-drawer__resize-trigger--hover`]),onMouseenter:this.handleMouseenterResizeTrigger,onMouseleave:this.handleMouseleaveResizeTrigger,onMousedown:this.handleMousedownResizeTrigger},null,42,yt)):null,this.nativeScrollbar?(d(),E("div",{key:3,class:B([`${r}-drawer-content-wrapper`,this.contentClass]),style:V(this.contentStyle),role:"none"},[I(()=>e.default?.())],6)):(d(),x(ke,re({key:4},this.scrollbarProps,{contentStyle:this.contentStyle,contentClass:[`${r}-drawer-content-wrapper`,this.contentClass],theme:this.mergedTheme.peers.Scrollbar,themeOverrides:this.mergedTheme.peerOverrides.Scrollbar}),me(e),1040,["contentStyle","contentClass","theme","themeOverrides"]))]),this.bodyDirectives)},1032,["name","appear","onAfterEnter","onAfterLeave"]))},1032,["disabled","active","autoFocus","onEsc"]))])),[[ge,this.displayDirective==="if"||this.displayed||this.show]]):null}});const{cubicBezierEaseIn:St,cubicBezierEaseOut:kt}=ne;function $t({duration:e="0.3s",leaveDuration:r="0.2s",name:o="slide-in-from-bottom"}={}){return[s(`&.${o}-transition-leave-active`,{transition:`transform ${r} ${St}`}),s(`&.${o}-transition-enter-active`,{transition:`transform ${e} ${kt}`}),s(`&.${o}-transition-enter-to`,{transform:"translateY(0)"}),s(`&.${o}-transition-enter-from`,{transform:"translateY(100%)"}),s(`&.${o}-transition-leave-from`,{transform:"translateY(0)"}),s(`&.${o}-transition-leave-to`,{transform:"translateY(100%)"})]}const{cubicBezierEaseIn:zt,cubicBezierEaseOut:xt}=ne;function Bt({duration:e="0.3s",leaveDuration:r="0.2s",name:o="slide-in-from-left"}={}){return[s(`&.${o}-transition-leave-active`,{transition:`transform ${r} ${zt}`}),s(`&.${o}-transition-enter-active`,{transition:`transform ${e} ${xt}`}),s(`&.${o}-transition-enter-to`,{transform:"translateX(0)"}),s(`&.${o}-transition-enter-from`,{transform:"translateX(-100%)"}),s(`&.${o}-transition-leave-from`,{transform:"translateX(0)"}),s(`&.${o}-transition-leave-to`,{transform:"translateX(-100%)"})]}const{cubicBezierEaseIn:Et,cubicBezierEaseOut:Rt}=ne;function _t({duration:e="0.3s",leaveDuration:r="0.2s",name:o="slide-in-from-right"}={}){return[s(`&.${o}-transition-leave-active`,{transition:`transform ${r} ${Et}`}),s(`&.${o}-transition-enter-active`,{transition:`transform ${e} ${Rt}`}),s(`&.${o}-transition-enter-to`,{transform:"translateX(0)"}),s(`&.${o}-transition-enter-from`,{transform:"translateX(100%)"}),s(`&.${o}-transition-leave-from`,{transform:"translateX(0)"}),s(`&.${o}-transition-leave-to`,{transform:"translateX(100%)"})]}const{cubicBezierEaseIn:Tt,cubicBezierEaseOut:Mt}=ne;function Pt({duration:e="0.3s",leaveDuration:r="0.2s",name:o="slide-in-from-top"}={}){return[s(`&.${o}-transition-leave-active`,{transition:`transform ${r} ${Tt}`}),s(`&.${o}-transition-enter-active`,{transition:`transform ${e} ${Mt}`}),s(`&.${o}-transition-enter-to`,{transform:"translateY(0)"}),s(`&.${o}-transition-enter-from`,{transform:"translateY(-100%)"}),s(`&.${o}-transition-leave-from`,{transform:"translateY(0)"}),s(`&.${o}-transition-leave-to`,{transform:"translateY(-100%)"})]}var Ft=s([z("drawer",`
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
 `,[_t(),Bt(),Pt(),$t(),H("unselectable",`
 user-select: none; 
 -webkit-user-select: none;
 `),H("native-scrollbar",[z("drawer-content-wrapper",`
 overflow: auto;
 height: 100%;
 `)]),W("resize-trigger",`
 position: absolute;
 background-color: #0000;
 transition: background-color .3s var(--n-bezier);
 `,[H("hover",`
 background-color: var(--n-resize-trigger-color-hover);
 `)]),z("drawer-content-wrapper",`
 box-sizing: border-box;
 `),z("drawer-content",`
 height: 100%;
 display: flex;
 flex-direction: column;
 `,[H("native-scrollbar",[z("drawer-body-content-wrapper",`
 height: 100%;
 overflow: auto;
 `)]),z("drawer-body",`
 flex: 1 0 0;
 overflow: hidden;
 `),z("drawer-body-content-wrapper",`
 box-sizing: border-box;
 padding: var(--n-body-padding);
 `),z("drawer-header",`
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
 `,[W("main",`
 flex: 1;
 `),W("close",`
 margin-left: 6px;
 transition:
 background-color .3s var(--n-bezier),
 color .3s var(--n-bezier);
 `)]),z("drawer-footer",`
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
 `,[W("resize-trigger",`
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
 `,[W("resize-trigger",`
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
 `,[W("resize-trigger",`
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
 `,[W("resize-trigger",`
 width: 100%;
 height: 3px;
 top: 0;
 left: 0;
 transform: translateY(-1.5px);
 cursor: ns-resize;
 `)])]),s("body",[s(">",[z("drawer-container",`
 position: fixed;
 `)])]),z("drawer-container",`
 position: relative;
 position: absolute;
 left: 0;
 right: 0;
 top: 0;
 bottom: 0;
 pointer-events: none;
 `,[s("> *",`
 pointer-events: all;
 `)]),z("drawer-mask",`
 background-color: rgba(0, 0, 0, .3);
 position: absolute;
 left: 0;
 right: 0;
 top: 0;
 bottom: 0;
 `,[H("invisible",`
 background-color: rgba(0, 0, 0, 0)
 `),qe({enterDuration:"0.2s",leaveDuration:"0.2s",enterCubicBezier:"var(--n-bezier-in)",leaveCubicBezier:"var(--n-bezier-out)"})])]);const Ot=["onClick"],Ht={...Be.props,show:Boolean,width:[Number,String],height:[Number,String],placement:{type:String,default:"right"},maskClosable:{type:Boolean,default:!0},showMask:{type:[Boolean,String],default:!0},to:[String,Object],displayDirective:{type:String,default:"if"},nativeScrollbar:{type:Boolean,default:!0},zIndex:Number,onMaskClick:Function,scrollbarProps:Object,contentClass:String,contentStyle:[Object,String],trapFocus:{type:Boolean,default:!0},onEsc:Function,autoFocus:{type:Boolean,default:!0},closeOnEsc:{type:Boolean,default:!0},blockScroll:{type:Boolean,default:!0},maxWidth:Number,maxHeight:Number,minWidth:Number,minHeight:Number,resizable:Boolean,defaultWidth:{type:[Number,String],default:251},defaultHeight:{type:[Number,String],default:251},onUpdateWidth:[Function,Array],onUpdateHeight:[Function,Array],"onUpdate:width":[Function,Array],"onUpdate:height":[Function,Array],"onUpdate:show":[Function,Array],onUpdateShow:[Function,Array],onAfterEnter:Function,onAfterLeave:Function,drawerStyle:[String,Object],drawerClass:String,target:null,onShow:Function,onHide:Function};var It=oe({name:"Drawer",inheritAttrs:!1,props:Ht,setup(e){const{mergedClsPrefixRef:r,namespaceRef:o,inlineThemeDisabled:c}=ze(e),b=Je(),C=Be("Drawer","-drawer",Ft,et,e,r),u=w(e.defaultWidth),g=w(e.defaultHeight),p=we(be(e,"width"),u),y=we(be(e,"height"),g),$=M(()=>{const{placement:a}=e;return a==="top"||a==="bottom"?"":ye(p.value)}),R=M(()=>{const{placement:a}=e;return a==="left"||a==="right"?"":ye(y.value)}),D=a=>{const{onUpdateWidth:m,"onUpdate:width":v}=e;m&&L(m,a),v&&L(v,a),u.value=a},P=a=>{const{onUpdateHeight:m,"onUpdate:width":v}=e;m&&L(m,a),v&&L(v,a),g.value=a},h=M(()=>[{width:$.value,height:R.value},e.drawerStyle||""]);function N(a){const{onMaskClick:m,maskClosable:v}=e;v&&A(!1),m&&m(a)}function q(a){N(a)}const K=Qe();function J(a){e.onEsc?.(),e.show&&e.closeOnEsc&&Ze(a)&&(K.value||A(!1))}function A(a){const{onHide:m,onUpdateShow:v,"onUpdate:show":i}=e;v&&L(v,a),i&&L(i,a),m&&!a&&L(m,a)}te(ve,{isMountedRef:b,mergedThemeRef:C,mergedClsPrefixRef:r,doUpdateShow:A,doUpdateHeight:P,doUpdateWidth:D});const U=M(()=>{const{common:{cubicBezierEaseInOut:a,cubicBezierEaseIn:m,cubicBezierEaseOut:v},self:{color:i,textColor:l,boxShadow:f,lineHeight:j,headerPadding:ae,footerPadding:se,borderRadius:ie,bodyPadding:le,titleFontSize:Z,titleTextColor:t,titleFontWeight:n,headerBorderBottom:O,footerBorderTop:X,closeIconColor:Y,closeIconColorHover:Ee,closeIconColorPressed:Re,closeColorHover:_e,closeColorPressed:Te,closeIconSize:Me,closeSize:Pe,closeBorderRadius:Fe,resizableTriggerColorHover:Oe}}=C.value;return{"--n-line-height":j,"--n-color":i,"--n-border-radius":ie,"--n-text-color":l,"--n-box-shadow":f,"--n-bezier":a,"--n-bezier-out":v,"--n-bezier-in":m,"--n-header-padding":ae,"--n-body-padding":le,"--n-footer-padding":se,"--n-title-text-color":t,"--n-title-font-size":Z,"--n-title-font-weight":n,"--n-header-border-bottom":O,"--n-footer-border-top":X,"--n-close-icon-color":Y,"--n-close-icon-color-hover":Ee,"--n-close-icon-color-pressed":Re,"--n-close-size":Pe,"--n-close-color-hover":_e,"--n-close-color-pressed":Te,"--n-close-icon-size":Me,"--n-close-border-radius":Fe,"--n-resize-trigger-color-hover":Oe}}),F=c?Ge("drawer",void 0,U,e):void 0;return{mergedClsPrefix:r,namespace:o,mergedBodyStyle:h,handleOutsideClick:q,handleMaskClick:N,handleEsc:J,mergedTheme:C,cssVars:c?void 0:U,themeClass:F?.themeClass,onRender:F?.onRender,isMounted:b}},render(){const{mergedClsPrefix:e}=this;return d(),x(Ke,{to:this.to,show:this.show},{default:()=>(this.onRender?.(),he((d(),E("div",{class:B([`${e}-drawer-container`,this.namespace,this.themeClass]),style:V(this.cssVars),role:"none"},[this.showMask?(d(),x(Se,{key:0,name:"fade-in-transition",appear:this.isMounted},{default:()=>this.show?(d(),E("div",{key:1,"aria-hidden":!0,class:B([`${e}-drawer-mask`,this.showMask==="transparent"&&`${e}-drawer-mask--invisible`]),onClick:this.handleMaskClick},null,10,Ot)):null},1032,["appear"])):I(()=>null),(d(),x(Ct,re(this.$attrs,{class:[this.drawerClass,this.$attrs.class],style:[this.mergedBodyStyle,this.$attrs.style],blockScroll:this.blockScroll,contentStyle:this.contentStyle,contentClass:this.contentClass,placement:this.placement,scrollbarProps:this.scrollbarProps,show:this.show,displayDirective:this.displayDirective,nativeScrollbar:this.nativeScrollbar,onAfterEnter:this.onAfterEnter,onAfterLeave:this.onAfterLeave,trapFocus:this.trapFocus,autoFocus:this.autoFocus,resizable:this.resizable,maxHeight:this.maxHeight,minHeight:this.minHeight,maxWidth:this.maxWidth,minWidth:this.minWidth,showMask:this.showMask,onEsc:this.handleEsc,onClickoutside:this.handleOutsideClick}),me(this.$slots),1040,["class","style","blockScroll","contentStyle","contentClass","placement","scrollbarProps","show","displayDirective","nativeScrollbar","onAfterEnter","onAfterLeave","trapFocus","autoFocus","resizable","maxHeight","minHeight","maxWidth","minWidth","showMask","onEsc","onClickoutside"]))],6)),[[tt,{zIndex:this.zIndex,enabled:this.show}]]))},1032,["to","show"])}});const Dt={title:String,headerClass:String,headerStyle:[Object,String],footerClass:String,footerStyle:[Object,String],bodyClass:String,bodyStyle:[Object,String],bodyContentClass:String,bodyContentStyle:[Object,String],nativeScrollbar:{type:Boolean,default:!0},scrollbarProps:Object,closable:Boolean};var At=oe({name:"DrawerContent",props:Dt,slots:Object,setup(){const e=$e(ve,null);e||ot("drawer-content","`n-drawer-content` must be placed inside `n-drawer`.");const{doUpdateShow:r}=e;function o(){r(!1)}return{handleCloseClick:o,mergedTheme:e.mergedThemeRef,mergedClsPrefix:e.mergedClsPrefixRef}},render(){const{title:e,mergedClsPrefix:r,nativeScrollbar:o,mergedTheme:c,bodyClass:b,bodyStyle:C,bodyContentClass:u,bodyContentStyle:g,headerClass:p,headerStyle:y,footerClass:$,footerStyle:R,scrollbarProps:D,closable:P,$slots:h}=this;return d(),E("div",{role:"none",class:B([`${r}-drawer-content`,o&&`${r}-drawer-content--native-scrollbar`])},[h.header||e||P?(d(),E("div",{key:0,class:B([`${r}-drawer-header`,p]),style:V(y),role:"none"},[G("div",{class:B(`${r}-drawer-header__main`),role:"heading","aria-level":"1"},[h.header!==void 0?(d(),E(pe,{key:0},[I(()=>h.header())],64)):(d(),E(pe,{key:1},[I(()=>e)],64))],2),I(()=>P&&(d(),x(rt,{onClick:this.handleCloseClick,clsPrefix:r,class:B(`${r}-drawer-header__close`),absolute:!0},null,8,["onClick","clsPrefix","class"])))],6)):I(()=>null),o?(d(),E("div",{key:2,class:B([`${r}-drawer-body`,b]),style:V(C),role:"none"},[G("div",{class:B([`${r}-drawer-body-content-wrapper`,u]),style:V(g),role:"none"},[I(()=>h.default?.())],6)],6)):(d(),x(ke,re({key:3,themeOverrides:c.peerOverrides.Scrollbar,theme:c.peers.Scrollbar},D,{class:`${r}-drawer-body`,contentClass:[`${r}-drawer-body-content-wrapper`,u],contentStyle:g}),me(h),1040,["themeOverrides","theme","class","contentClass","contentStyle"])),h.footer?(d(),E("div",{key:4,class:B([`${r}-drawer-footer`,$]),style:V(R),role:"none"},[I(()=>h.footer())],6)):I(()=>null)],2)}});const Wt={class:"breadcrumb","aria-label":"Breadcrumb"},Lt={class:"muted"},Nt=5e3,Ut=oe({__name:"ContainersPage",setup(e){const r=lt(),o=ct(),c=M(()=>String(r.params.id??"")),b=w([]),C=w(!1),u=w(null),g=w(""),p=w(!1),y=w({}),$=w(null),R=w(!1),D=bt("(max-width: 760px)"),P=M(()=>D.value?"94vw":720);let h=null;function N(t){switch(t.trim().toLowerCase()){case"running":return"success";case"restarting":case"paused":case"created":return"warning";case"exited":case"dead":return"error";default:return"default"}}function q(t){return t.trim().toLowerCase()==="running"}function K(t,n){return y.value[`${t}:${n}`]===!0}function J(t){return Object.keys(y.value).some(n=>n.startsWith(`${t}:`))}function A(t){const n=t.status||t.state||"unknown";return T("span",{title:n},[T(Ie,{type:N(t.state),size:"small",round:!0},{default:()=>t.state||"unknown"})])}function U(t){return!t.ports||t.ports.length===0?T(fe,{depth:3},{default:()=>"—"}):T("span",{class:"mono"},t.ports.join(", "))}function F(t,n,O){return T(ue,{size:"small",secondary:!0,loading:K(t.id,n),disabled:J(t.id),onClick:X=>{X.stopPropagation(),se(t,n,O)}},{default:()=>O})}function a(t){return T(ue,{size:"small",quaternary:!0,onClick:n=>{n.stopPropagation(),f(t)}},{default:()=>"Logs"})}function m(t){const n=[];return q(t.state)?(n.push(F(t,"restart","Restart")),n.push(F(t,"stop","Stop"))):n.push(F(t,"start","Start")),n.push(a(t)),T(ee,{size:8,align:"center",wrap:!1},{default:()=>n})}const v=[{title:"Name",key:"name",minWidth:180,ellipsis:{tooltip:!0},render:t=>T("span",{class:"mono"},t.name||t.id)},{title:"Image",key:"image",minWidth:180,ellipsis:{tooltip:!0},render:t=>T("span",{class:"mono muted"},t.image||"—")},{title:"State",key:"state",width:140,render:t=>A(t)},{title:"Ports",key:"ports",minWidth:140,render:t=>U(t)},{title:"Actions",key:"actions",width:220,render:t=>m(t)}];function i(t){return t.id}function l(t){return{style:"cursor: pointer;",onClick:()=>f(t)}}function f(t){$.value=t,R.value=!0}async function j(t){if(c.value){t&&(C.value=!0);try{b.value=await ut(c.value),u.value=null,p.value=!0}catch(n){u.value=Ce(n)}finally{C.value=!1}}}async function ae(){if(c.value)try{const t=await gt(c.value);g.value=t.name}catch{}}async function se(t,n,O){const X=`${t.id}:${n}`;y.value={...y.value,[X]:!0};try{n==="start"?await ht(c.value,t.id):n==="stop"?await mt(c.value,t.id):await vt(c.value,t.id),o.success(`${O} requested for ${t.name}`),await j(!1)}catch(Y){o.error(Ce(Y))}finally{const Y={...y.value};delete Y[X],y.value=Y}}function ie(){h===null&&(h=setInterval(()=>{j(!1)},Nt))}function le(){h!==null&&(clearInterval(h),h=null)}async function Z(){p.value=!1,await Promise.all([ae(),j(!0)])}return xe(c,()=>{$.value=null,R.value=!1,Z()}),nt(()=>{Z(),ie()}),at(()=>{le()}),(t,n)=>(d(),x(k(ee),{vertical:"",size:16},{default:S(()=>[G("nav",Wt,[_(k(st),{to:"/servers"},{default:S(()=>[...n[2]||(n[2]=[Q("Servers",-1)])]),_:1}),n[3]||(n[3]=G("span",{class:"breadcrumb__sep"},"/",-1)),G("span",Lt,de(g.value||c.value),1)]),_(k(it),null,{header:S(()=>[_(k(ee),{align:"center",justify:"space-between"},{default:S(()=>[_(k(ee),{align:"center",size:10},{default:S(()=>[_(k(fe),{strong:""},{default:S(()=>[...n[4]||(n[4]=[Q("Containers",-1)])]),_:1}),g.value?(d(),x(k(fe),{key:0,depth:"3"},{default:S(()=>[Q(de(g.value),1)]),_:1})):ce("",!0)]),_:1}),_(k(ue),{secondary:"",loading:C.value,onClick:n[0]||(n[0]=O=>j(!0))},{default:S(()=>[...n[5]||(n[5]=[Q(" Refresh ",-1)])]),_:1},8,["loading"])]),_:1})]),default:S(()=>[u.value?(d(),x(k(De),{key:0,type:"error","show-icon":!0,style:{"margin-bottom":"12px"}},{default:S(()=>[Q(de(u.value),1)]),_:1})):ce("",!0),_(k(dt),{columns:v,data:b.value,loading:C.value,"row-key":i,"row-props":l,bordered:!1,"scroll-x":960,pagination:{pageSize:10}},{empty:S(()=>[_(k(He),{description:p.value?"No containers on this node.":"Loading containers…"},null,8,["description"])]),_:1},8,["data","loading"])]),_:1}),_(k(It),{show:R.value,"onUpdate:show":n[1]||(n[1]=O=>R.value=O),width:P.value,placement:"right"},{default:S(()=>[_(k(At),{closable:"","native-scrollbar":!1},{default:S(()=>[$.value?(d(),x(ft,{key:0,"server-id":c.value,"container-id":$.value.id,title:$.value.name,subtitle:$.value.image,"auto-start-stream":!0},null,8,["server-id","container-id","title","subtitle"])):ce("",!0)]),_:1})]),_:1},8,["show","width"])]),_:1}))}}),gr=pt(Ut,[["__scopeId","data-v-41da2b23"]]);export{gr as default};
