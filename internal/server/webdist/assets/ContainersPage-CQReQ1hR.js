import{E as Ae}from"./Empty-BIhfpyXV.js";import{T as De}from"./Tag-ATQLBOJm.js";import{A as We}from"./Alert-DkQ83aSd.js";import{d as oe,a6 as he,a7 as be,o as d,c as E,m as x,a8 as $e,K as T,U as B,T as V,M as I,a9 as ke,L as re,a0 as me,aa as Le,q as w,ab as ze,ac as ge,N as xe,ad as Ne,ae as Ue,z as Be,a4 as je,af as Xe,y as M,ag as Ye,ah as te,ai as Ve,aj as qe,ak as Ke,H as s,al as ne,I as z,$ as O,am as W,an as Je,J as Ee,ao as Qe,ap as Ge,aq as Ze,a1 as et,ar as tt,as as L,at as rt,au as ot,av as pe,a as G,F as ye,aw as nt,ax as at,a3 as Re,g as st,A as it,w as S,u as $,e as _,k as Q,j as lt,t as de,l as ce,B as ue,C as dt,h as ct}from"./index-DGkcCjr4.js";import{D as ut}from"./DataTable-CTG-qiU5.js";import{u as ft}from"./use-message-RliEajM1.js";import{f as we}from"./format-length-CpShgGjT.js";import{u as Ce}from"./use-merged-state-BVkeprVA.js";import{S as ee}from"./Space-CsaG4A2Q.js";import{t as fe}from"./text-Cy_YmYUH.js";import{i as ht,g as mt}from"./servers-BwlXy2kO.js";import{u as gt}from"./useMediaQuery-B9CaCd1z.js";import{L as vt}from"./LogViewer-DI6SiQC9.js";import{_ as bt}from"./_plugin-vue_export-helper-DlAUqK2U.js";import"./Pagination-CWvjKfKG.js";import"./Popover-WssYa72p.js";import"./Input-D9C1_Re1.js";import"./create-ref-setter-C4J8sofl.js";import"./Select-WJPHKYNv.js";import"./create-DLefnRiD.js";import"./CheckboxGroup-OndKkvfu.js";import"./Tooltip-BrpqHbZR.js";import"./RadioGroup-idDnjaSi.js";import"./Dropdown-DPxm4Psn.js";import"./ChevronRight-DofGfZNO.js";import"./Icon-ClwKjzX_.js";const pt=["onMouseenter","onMouseleave","onMousedown"],yt={key:1,role:"none"};var wt=oe({name:"NDrawerContent",inheritAttrs:!1,props:{blockScroll:Boolean,show:{type:Boolean,default:void 0},displayDirective:{type:String,required:!0},placement:{type:String,required:!0},contentClass:String,contentStyle:[Object,String],nativeScrollbar:{type:Boolean,required:!0},scrollbarProps:Object,trapFocus:{type:Boolean,default:!0},autoFocus:{type:Boolean,default:!0},showMask:{type:[Boolean,String],required:!0},maxWidth:Number,maxHeight:Number,minWidth:Number,minHeight:Number,resizable:Boolean,onClickoutside:Function,onAfterLeave:Function,onAfterEnter:Function,onEsc:Function},setup(e){const r=w(!!e.show),o=w(null),c=ze(ge);let b=0,C="",u=null;const v=w(!1),p=w(!1),y=M(()=>e.placement==="top"||e.placement==="bottom"),{mergedClsPrefixRef:k,mergedRtlRef:R}=xe(e),A=Ne("Drawer",R,k),P=a,h=i=>{p.value=!0,b=y.value?i.clientY:i.clientX,C=document.body.style.cursor,document.body.style.cursor=y.value?"ns-resize":"ew-resize",document.body.addEventListener("mousemove",F),document.body.addEventListener("mouseleave",P),document.body.addEventListener("mouseup",a)},N=()=>{u!==null&&(window.clearTimeout(u),u=null),p.value?v.value=!0:u=window.setTimeout(()=>{v.value=!0},300)},q=()=>{u!==null&&(window.clearTimeout(u),u=null),v.value=!1},{doUpdateHeight:K,doUpdateWidth:J}=c,D=i=>{const{maxWidth:l}=e;if(l&&i>l)return l;const{minWidth:f}=e;return f&&i<f?f:i},U=i=>{const{maxHeight:l}=e;if(l&&i>l)return l;const{minHeight:f}=e;return f&&i<f?f:i};function F(i){if(p.value)if(y.value){let l=o.value?.offsetHeight||0;const f=b-i.clientY;l+=e.placement==="bottom"?f:-f,l=U(l),K(l),b=i.clientY}else{let l=o.value?.offsetWidth||0;const f=b-i.clientX;l+=e.placement==="right"?f:-f,l=D(l),J(l),b=i.clientX}}function a(){p.value&&(b=0,p.value=!1,document.body.style.cursor=C,document.body.removeEventListener("mousemove",F),document.body.removeEventListener("mouseup",a),document.body.removeEventListener("mouseleave",P))}Ue(()=>{e.show&&(r.value=!0)}),Be(()=>e.show,i=>{i||a()}),je(()=>{a()});const m=M(()=>{const{show:i}=e,l=[[be,i]];return e.showMask||l.push([Ye,e.onClickoutside,void 0,{capture:!0}]),l});function g(){r.value=!1,e.onAfterLeave?.()}return Xe(M(()=>e.blockScroll&&r.value)),te(Ve,o),te(qe,null),te(Ke,null),{bodyRef:o,rtlEnabled:A,mergedClsPrefix:c.mergedClsPrefixRef,isMounted:c.isMountedRef,mergedTheme:c.mergedThemeRef,displayed:r,transitionName:M(()=>({right:"slide-in-from-right-transition",left:"slide-in-from-left-transition",top:"slide-in-from-top-transition",bottom:"slide-in-from-bottom-transition"})[e.placement]),handleAfterLeave:g,bodyDirectives:m,handleMousedownResizeTrigger:h,handleMouseenterResizeTrigger:N,handleMouseleaveResizeTrigger:q,isDragging:p,isHoverOnResizeTrigger:v}},render(){const{$slots:e,mergedClsPrefix:r}=this;return this.displayDirective==="show"||this.displayed||this.show?he((d(),E("div",yt,[(d(),x(Le,{disabled:!this.showMask||!this.trapFocus,active:this.show,autoFocus:this.autoFocus,onEsc:this.onEsc},{default:()=>(d(),x($e,{name:this.transitionName,appear:this.isMounted,onAfterEnter:this.onAfterEnter,onAfterLeave:this.handleAfterLeave},{default:()=>he(T("div",re(this.$attrs,{role:"dialog",ref:"bodyRef","aria-modal":"true",class:[`${r}-drawer`,this.rtlEnabled&&`${r}-drawer--rtl`,`${r}-drawer--${this.placement}-placement`,this.isDragging&&`${r}-drawer--unselectable`,this.nativeScrollbar&&`${r}-drawer--native-scrollbar`]}),[this.resizable?(d(),E("div",{key:2,class:B([`${r}-drawer__resize-trigger`,(this.isDragging||this.isHoverOnResizeTrigger)&&`${r}-drawer__resize-trigger--hover`]),onMouseenter:this.handleMouseenterResizeTrigger,onMouseleave:this.handleMouseleaveResizeTrigger,onMousedown:this.handleMousedownResizeTrigger},null,42,pt)):null,this.nativeScrollbar?(d(),E("div",{key:3,class:B([`${r}-drawer-content-wrapper`,this.contentClass]),style:V(this.contentStyle),role:"none"},[I(()=>e.default?.())],6)):(d(),x(ke,re({key:4},this.scrollbarProps,{contentStyle:this.contentStyle,contentClass:[`${r}-drawer-content-wrapper`,this.contentClass],theme:this.mergedTheme.peers.Scrollbar,themeOverrides:this.mergedTheme.peerOverrides.Scrollbar}),me(e),1040,["contentStyle","contentClass","theme","themeOverrides"]))]),this.bodyDirectives)},1032,["name","appear","onAfterEnter","onAfterLeave"]))},1032,["disabled","active","autoFocus","onEsc"]))])),[[be,this.displayDirective==="if"||this.displayed||this.show]]):null}});const{cubicBezierEaseIn:Ct,cubicBezierEaseOut:St}=ne;function $t({duration:e="0.3s",leaveDuration:r="0.2s",name:o="slide-in-from-bottom"}={}){return[s(`&.${o}-transition-leave-active`,{transition:`transform ${r} ${Ct}`}),s(`&.${o}-transition-enter-active`,{transition:`transform ${e} ${St}`}),s(`&.${o}-transition-enter-to`,{transform:"translateY(0)"}),s(`&.${o}-transition-enter-from`,{transform:"translateY(100%)"}),s(`&.${o}-transition-leave-from`,{transform:"translateY(0)"}),s(`&.${o}-transition-leave-to`,{transform:"translateY(100%)"})]}const{cubicBezierEaseIn:kt,cubicBezierEaseOut:zt}=ne;function xt({duration:e="0.3s",leaveDuration:r="0.2s",name:o="slide-in-from-left"}={}){return[s(`&.${o}-transition-leave-active`,{transition:`transform ${r} ${kt}`}),s(`&.${o}-transition-enter-active`,{transition:`transform ${e} ${zt}`}),s(`&.${o}-transition-enter-to`,{transform:"translateX(0)"}),s(`&.${o}-transition-enter-from`,{transform:"translateX(-100%)"}),s(`&.${o}-transition-leave-from`,{transform:"translateX(0)"}),s(`&.${o}-transition-leave-to`,{transform:"translateX(-100%)"})]}const{cubicBezierEaseIn:Bt,cubicBezierEaseOut:Et}=ne;function Rt({duration:e="0.3s",leaveDuration:r="0.2s",name:o="slide-in-from-right"}={}){return[s(`&.${o}-transition-leave-active`,{transition:`transform ${r} ${Bt}`}),s(`&.${o}-transition-enter-active`,{transition:`transform ${e} ${Et}`}),s(`&.${o}-transition-enter-to`,{transform:"translateX(0)"}),s(`&.${o}-transition-enter-from`,{transform:"translateX(100%)"}),s(`&.${o}-transition-leave-from`,{transform:"translateX(0)"}),s(`&.${o}-transition-leave-to`,{transform:"translateX(100%)"})]}const{cubicBezierEaseIn:_t,cubicBezierEaseOut:Tt}=ne;function Mt({duration:e="0.3s",leaveDuration:r="0.2s",name:o="slide-in-from-top"}={}){return[s(`&.${o}-transition-leave-active`,{transition:`transform ${r} ${_t}`}),s(`&.${o}-transition-enter-active`,{transition:`transform ${e} ${Tt}`}),s(`&.${o}-transition-enter-to`,{transform:"translateY(0)"}),s(`&.${o}-transition-enter-from`,{transform:"translateY(-100%)"}),s(`&.${o}-transition-leave-from`,{transform:"translateY(0)"}),s(`&.${o}-transition-leave-to`,{transform:"translateY(-100%)"})]}var Pt=s([z("drawer",`
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
 `,[Rt(),xt(),Mt(),$t(),O("unselectable",`
 user-select: none; 
 -webkit-user-select: none;
 `),O("native-scrollbar",[z("drawer-content-wrapper",`
 overflow: auto;
 height: 100%;
 `)]),W("resize-trigger",`
 position: absolute;
 background-color: #0000;
 transition: background-color .3s var(--n-bezier);
 `,[O("hover",`
 background-color: var(--n-resize-trigger-color-hover);
 `)]),z("drawer-content-wrapper",`
 box-sizing: border-box;
 `),z("drawer-content",`
 height: 100%;
 display: flex;
 flex-direction: column;
 `,[O("native-scrollbar",[z("drawer-body-content-wrapper",`
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
 `)]),O("right-placement",`
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
 `)]),O("left-placement",`
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
 `)]),O("top-placement",`
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
 `)]),O("bottom-placement",`
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
 `,[O("invisible",`
 background-color: rgba(0, 0, 0, 0)
 `),Je({enterDuration:"0.2s",leaveDuration:"0.2s",enterCubicBezier:"var(--n-bezier-in)",leaveCubicBezier:"var(--n-bezier-out)"})])]);const Ft=["onClick"],Ht={...Ee.props,show:Boolean,width:[Number,String],height:[Number,String],placement:{type:String,default:"right"},maskClosable:{type:Boolean,default:!0},showMask:{type:[Boolean,String],default:!0},to:[String,Object],displayDirective:{type:String,default:"if"},nativeScrollbar:{type:Boolean,default:!0},zIndex:Number,onMaskClick:Function,scrollbarProps:Object,contentClass:String,contentStyle:[Object,String],trapFocus:{type:Boolean,default:!0},onEsc:Function,autoFocus:{type:Boolean,default:!0},closeOnEsc:{type:Boolean,default:!0},blockScroll:{type:Boolean,default:!0},maxWidth:Number,maxHeight:Number,minWidth:Number,minHeight:Number,resizable:Boolean,defaultWidth:{type:[Number,String],default:251},defaultHeight:{type:[Number,String],default:251},onUpdateWidth:[Function,Array],onUpdateHeight:[Function,Array],"onUpdate:width":[Function,Array],"onUpdate:height":[Function,Array],"onUpdate:show":[Function,Array],onUpdateShow:[Function,Array],onAfterEnter:Function,onAfterLeave:Function,drawerStyle:[String,Object],drawerClass:String,target:null,onShow:Function,onHide:Function};var Ot=oe({name:"Drawer",inheritAttrs:!1,props:Ht,setup(e){const{mergedClsPrefixRef:r,namespaceRef:o,inlineThemeDisabled:c}=xe(e),b=Ge(),C=Ee("Drawer","-drawer",Pt,rt,e,r),u=w(e.defaultWidth),v=w(e.defaultHeight),p=Ce(pe(e,"width"),u),y=Ce(pe(e,"height"),v),k=M(()=>{const{placement:a}=e;return a==="top"||a==="bottom"?"":we(p.value)}),R=M(()=>{const{placement:a}=e;return a==="left"||a==="right"?"":we(y.value)}),A=a=>{const{onUpdateWidth:m,"onUpdate:width":g}=e;m&&L(m,a),g&&L(g,a),u.value=a},P=a=>{const{onUpdateHeight:m,"onUpdate:width":g}=e;m&&L(m,a),g&&L(g,a),v.value=a},h=M(()=>[{width:k.value,height:R.value},e.drawerStyle||""]);function N(a){const{onMaskClick:m,maskClosable:g}=e;g&&D(!1),m&&m(a)}function q(a){N(a)}const K=Ze();function J(a){e.onEsc?.(),e.show&&e.closeOnEsc&&tt(a)&&(K.value||D(!1))}function D(a){const{onHide:m,onUpdateShow:g,"onUpdate:show":i}=e;g&&L(g,a),i&&L(i,a),m&&!a&&L(m,a)}te(ge,{isMountedRef:b,mergedThemeRef:C,mergedClsPrefixRef:r,doUpdateShow:D,doUpdateHeight:P,doUpdateWidth:A});const U=M(()=>{const{common:{cubicBezierEaseInOut:a,cubicBezierEaseIn:m,cubicBezierEaseOut:g},self:{color:i,textColor:l,boxShadow:f,lineHeight:j,headerPadding:ae,footerPadding:se,borderRadius:ie,bodyPadding:le,titleFontSize:Z,titleTextColor:t,titleFontWeight:n,headerBorderBottom:H,footerBorderTop:X,closeIconColor:Y,closeIconColorHover:_e,closeIconColorPressed:Te,closeColorHover:Me,closeColorPressed:Pe,closeIconSize:Fe,closeSize:He,closeBorderRadius:Oe,resizableTriggerColorHover:Ie}}=C.value;return{"--n-line-height":j,"--n-color":i,"--n-border-radius":ie,"--n-text-color":l,"--n-box-shadow":f,"--n-bezier":a,"--n-bezier-out":g,"--n-bezier-in":m,"--n-header-padding":ae,"--n-body-padding":le,"--n-footer-padding":se,"--n-title-text-color":t,"--n-title-font-size":Z,"--n-title-font-weight":n,"--n-header-border-bottom":H,"--n-footer-border-top":X,"--n-close-icon-color":Y,"--n-close-icon-color-hover":_e,"--n-close-icon-color-pressed":Te,"--n-close-size":He,"--n-close-color-hover":Me,"--n-close-color-pressed":Pe,"--n-close-icon-size":Fe,"--n-close-border-radius":Oe,"--n-resize-trigger-color-hover":Ie}}),F=c?et("drawer",void 0,U,e):void 0;return{mergedClsPrefix:r,namespace:o,mergedBodyStyle:h,handleOutsideClick:q,handleMaskClick:N,handleEsc:J,mergedTheme:C,cssVars:c?void 0:U,themeClass:F?.themeClass,onRender:F?.onRender,isMounted:b}},render(){const{mergedClsPrefix:e}=this;return d(),x(Qe,{to:this.to,show:this.show},{default:()=>(this.onRender?.(),he((d(),E("div",{class:B([`${e}-drawer-container`,this.namespace,this.themeClass]),style:V(this.cssVars),role:"none"},[this.showMask?(d(),x($e,{key:0,name:"fade-in-transition",appear:this.isMounted},{default:()=>this.show?(d(),E("div",{key:1,"aria-hidden":!0,class:B([`${e}-drawer-mask`,this.showMask==="transparent"&&`${e}-drawer-mask--invisible`]),onClick:this.handleMaskClick},null,10,Ft)):null},1032,["appear"])):I(()=>null),(d(),x(wt,re(this.$attrs,{class:[this.drawerClass,this.$attrs.class],style:[this.mergedBodyStyle,this.$attrs.style],blockScroll:this.blockScroll,contentStyle:this.contentStyle,contentClass:this.contentClass,placement:this.placement,scrollbarProps:this.scrollbarProps,show:this.show,displayDirective:this.displayDirective,nativeScrollbar:this.nativeScrollbar,onAfterEnter:this.onAfterEnter,onAfterLeave:this.onAfterLeave,trapFocus:this.trapFocus,autoFocus:this.autoFocus,resizable:this.resizable,maxHeight:this.maxHeight,minHeight:this.minHeight,maxWidth:this.maxWidth,minWidth:this.minWidth,showMask:this.showMask,onEsc:this.handleEsc,onClickoutside:this.handleOutsideClick}),me(this.$slots),1040,["class","style","blockScroll","contentStyle","contentClass","placement","scrollbarProps","show","displayDirective","nativeScrollbar","onAfterEnter","onAfterLeave","trapFocus","autoFocus","resizable","maxHeight","minHeight","maxWidth","minWidth","showMask","onEsc","onClickoutside"]))],6)),[[ot,{zIndex:this.zIndex,enabled:this.show}]]))},1032,["to","show"])}});const It={title:String,headerClass:String,headerStyle:[Object,String],footerClass:String,footerStyle:[Object,String],bodyClass:String,bodyStyle:[Object,String],bodyContentClass:String,bodyContentStyle:[Object,String],nativeScrollbar:{type:Boolean,default:!0},scrollbarProps:Object,closable:Boolean};var At=oe({name:"DrawerContent",props:It,slots:Object,setup(){const e=ze(ge,null);e||at("drawer-content","`n-drawer-content` must be placed inside `n-drawer`.");const{doUpdateShow:r}=e;function o(){r(!1)}return{handleCloseClick:o,mergedTheme:e.mergedThemeRef,mergedClsPrefix:e.mergedClsPrefixRef}},render(){const{title:e,mergedClsPrefix:r,nativeScrollbar:o,mergedTheme:c,bodyClass:b,bodyStyle:C,bodyContentClass:u,bodyContentStyle:v,headerClass:p,headerStyle:y,footerClass:k,footerStyle:R,scrollbarProps:A,closable:P,$slots:h}=this;return d(),E("div",{role:"none",class:B([`${r}-drawer-content`,o&&`${r}-drawer-content--native-scrollbar`])},[h.header||e||P?(d(),E("div",{key:0,class:B([`${r}-drawer-header`,p]),style:V(y),role:"none"},[G("div",{class:B(`${r}-drawer-header__main`),role:"heading","aria-level":"1"},[h.header!==void 0?(d(),E(ye,{key:0},[I(()=>h.header())],64)):(d(),E(ye,{key:1},[I(()=>e)],64))],2),I(()=>P&&(d(),x(nt,{onClick:this.handleCloseClick,clsPrefix:r,class:B(`${r}-drawer-header__close`),absolute:!0},null,8,["onClick","clsPrefix","class"])))],6)):I(()=>null),o?(d(),E("div",{key:2,class:B([`${r}-drawer-body`,b]),style:V(C),role:"none"},[G("div",{class:B([`${r}-drawer-body-content-wrapper`,u]),style:V(v),role:"none"},[I(()=>h.default?.())],6)],6)):(d(),x(ke,re({key:3,themeOverrides:c.peerOverrides.Scrollbar,theme:c.peers.Scrollbar},A,{class:`${r}-drawer-body`,contentClass:[`${r}-drawer-body-content-wrapper`,u],contentStyle:v}),me(h),1040,["themeOverrides","theme","class","contentClass","contentStyle"])),h.footer?(d(),E("div",{key:4,class:B([`${r}-drawer-footer`,k]),style:V(R),role:"none"},[I(()=>h.footer())],6)):I(()=>null)],2)}});async function Dt(e){return(await Re.get(`/servers/${e}/containers`)).data.containers??[]}async function ve(e,r,o){return(await Re.post(`/servers/${e}/containers/${encodeURIComponent(r)}/${o}`,{})).data.container_id}function Wt(e,r){return ve(e,r,"start")}function Lt(e,r){return ve(e,r,"stop")}function Nt(e,r){return ve(e,r,"restart")}function Se(e){return ht(e)?e.message||"Request failed":e instanceof Error?e.message:"Something went wrong. Please try again."}const Ut={class:"breadcrumb","aria-label":"Breadcrumb"},jt={class:"muted"},Xt=5e3,Yt=oe({__name:"ContainersPage",setup(e){const r=ct(),o=ft(),c=M(()=>String(r.params.id??"")),b=w([]),C=w(!1),u=w(null),v=w(""),p=w(!1),y=w({}),k=w(null),R=w(!1),A=gt("(max-width: 760px)"),P=M(()=>A.value?"94vw":720);let h=null;function N(t){switch(t.trim().toLowerCase()){case"running":return"success";case"restarting":case"paused":case"created":return"warning";case"exited":case"dead":return"error";default:return"default"}}function q(t){return t.trim().toLowerCase()==="running"}function K(t,n){return y.value[`${t}:${n}`]===!0}function J(t){return Object.keys(y.value).some(n=>n.startsWith(`${t}:`))}function D(t){const n=t.status||t.state||"unknown";return T("span",{title:n},[T(De,{type:N(t.state),size:"small",round:!0},{default:()=>t.state||"unknown"})])}function U(t){return!t.ports||t.ports.length===0?T(fe,{depth:3},{default:()=>"—"}):T("span",{class:"mono"},t.ports.join(", "))}function F(t,n,H){return T(ue,{size:"small",secondary:!0,loading:K(t.id,n),disabled:J(t.id),onClick:X=>{X.stopPropagation(),se(t,n,H)}},{default:()=>H})}function a(t){return T(ue,{size:"small",quaternary:!0,onClick:n=>{n.stopPropagation(),f(t)}},{default:()=>"Logs"})}function m(t){const n=[];return q(t.state)?(n.push(F(t,"restart","Restart")),n.push(F(t,"stop","Stop"))):n.push(F(t,"start","Start")),n.push(a(t)),T(ee,{size:8,align:"center",wrap:!1},{default:()=>n})}const g=[{title:"Name",key:"name",minWidth:180,ellipsis:{tooltip:!0},render:t=>T("span",{class:"mono"},t.name||t.id)},{title:"Image",key:"image",minWidth:180,ellipsis:{tooltip:!0},render:t=>T("span",{class:"mono muted"},t.image||"—")},{title:"State",key:"state",width:140,render:t=>D(t)},{title:"Ports",key:"ports",minWidth:140,render:t=>U(t)},{title:"Actions",key:"actions",width:220,render:t=>m(t)}];function i(t){return t.id}function l(t){return{style:"cursor: pointer;",onClick:()=>f(t)}}function f(t){k.value=t,R.value=!0}async function j(t){if(c.value){t&&(C.value=!0);try{b.value=await Dt(c.value),u.value=null,p.value=!0}catch(n){u.value=Se(n)}finally{C.value=!1}}}async function ae(){if(c.value)try{const t=await mt(c.value);v.value=t.name}catch{}}async function se(t,n,H){const X=`${t.id}:${n}`;y.value={...y.value,[X]:!0};try{n==="start"?await Wt(c.value,t.id):n==="stop"?await Lt(c.value,t.id):await Nt(c.value,t.id),o.success(`${H} requested for ${t.name}`),await j(!1)}catch(Y){o.error(Se(Y))}finally{const Y={...y.value};delete Y[X],y.value=Y}}function ie(){h===null&&(h=setInterval(()=>{j(!1)},Xt))}function le(){h!==null&&(clearInterval(h),h=null)}async function Z(){p.value=!1,await Promise.all([ae(),j(!0)])}return Be(c,()=>{k.value=null,R.value=!1,Z()}),st(()=>{Z(),ie()}),it(()=>{le()}),(t,n)=>(d(),x($(ee),{vertical:"",size:16},{default:S(()=>[G("nav",Ut,[_($(lt),{to:"/servers"},{default:S(()=>[...n[2]||(n[2]=[Q("Servers",-1)])]),_:1}),n[3]||(n[3]=G("span",{class:"breadcrumb__sep"},"/",-1)),G("span",jt,de(v.value||c.value),1)]),_($(dt),null,{header:S(()=>[_($(ee),{align:"center",justify:"space-between"},{default:S(()=>[_($(ee),{align:"center",size:10},{default:S(()=>[_($(fe),{strong:""},{default:S(()=>[...n[4]||(n[4]=[Q("Containers",-1)])]),_:1}),v.value?(d(),x($(fe),{key:0,depth:"3"},{default:S(()=>[Q(de(v.value),1)]),_:1})):ce("",!0)]),_:1}),_($(ue),{secondary:"",loading:C.value,onClick:n[0]||(n[0]=H=>j(!0))},{default:S(()=>[...n[5]||(n[5]=[Q(" Refresh ",-1)])]),_:1},8,["loading"])]),_:1})]),default:S(()=>[u.value?(d(),x($(We),{key:0,type:"error","show-icon":!0,style:{"margin-bottom":"12px"}},{default:S(()=>[Q(de(u.value),1)]),_:1})):ce("",!0),_($(ut),{columns:g,data:b.value,loading:C.value,"row-key":i,"row-props":l,bordered:!1,"scroll-x":960,pagination:{pageSize:10}},{empty:S(()=>[_($(Ae),{description:p.value?"No containers on this node.":"Loading containers…"},null,8,["description"])]),_:1},8,["data","loading"])]),_:1}),_($(Ot),{show:R.value,"onUpdate:show":n[1]||(n[1]=H=>R.value=H),width:P.value,placement:"right"},{default:S(()=>[_($(At),{closable:"","native-scrollbar":!1},{default:S(()=>[k.value?(d(),x(vt,{key:0,"server-id":c.value,"container-id":k.value.id,title:k.value.name,subtitle:k.value.image},null,8,["server-id","container-id","title","subtitle"])):ce("",!0)]),_:1})]),_:1},8,["show","width"])]),_:1}))}}),yr=bt(Yt,[["__scopeId","data-v-5ceaa3bb"]]);export{yr as default};
