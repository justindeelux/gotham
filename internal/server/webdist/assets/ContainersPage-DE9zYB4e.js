import{E as Ae}from"./Empty-mWahqhxO.js";import{i as De,g as We,T as Le}from"./servers-C1zHN2y8.js";import{A as Ne}from"./Alert-Chy798tk.js";import{d as ne,a4 as he,a5 as be,o as d,c as E,l as x,a6 as $e,K as T,S as B,V,M as I,a7 as ke,L as re,a1 as me,a8 as Ue,q as w,a9 as ze,aa as ge,N as xe,ab as je,ac as Xe,z as Be,ad as Ye,ae as Ve,y as M,af as qe,ag as te,ah as Ke,ai as Je,aj as Qe,H as s,ak as oe,I as z,a0 as O,al as W,am as Ge,J as Ee,an as Ze,ao as et,ap as tt,a2 as rt,aq as nt,ar as L,as as ot,at,au as pe,a as G,F as ye,av as st,aw as it,ax as Re,g as lt,A as dt,w as S,u as $,e as _,k as Q,j as ct,t as de,m as ce,B as ue,C as ut,h as ft}from"./index-9OL07nsM.js";import{D as ht}from"./DataTable-BMdcJY8_.js";import{u as mt}from"./use-message-CseHgIfu.js";import{f as we}from"./format-length-DGizkYws.js";import{u as Ce}from"./use-merged-state-DXJET7A8.js";import{S as ee,t as fe}from"./text-BDgNTOHQ.js";import{u as gt}from"./useMediaQuery-BI8POBnp.js";import{L as vt}from"./LogViewer-DLIbvm7a.js";import{_ as bt}from"./_plugin-vue_export-helper-DlAUqK2U.js";import"./Popover-CT7Yl5No.js";import"./Input-BPOoHNko.js";import"./Dropdown-DJEprZrY.js";import"./ChevronRight-BCcAkQ67.js";import"./Icon-C5TFf3Xb.js";import"./create-DLefnRiD.js";import"./Select-D2ZTnlcw.js";import"./CheckboxGroup-Bo7wCsFD.js";import"./Tooltip-BRs1AiHn.js";import"./RadioGroup-BEgdXq5Z.js";const pt=["onMouseenter","onMouseleave","onMousedown"],yt={key:1,role:"none"};var wt=ne({name:"NDrawerContent",inheritAttrs:!1,props:{blockScroll:Boolean,show:{type:Boolean,default:void 0},displayDirective:{type:String,required:!0},placement:{type:String,required:!0},contentClass:String,contentStyle:[Object,String],nativeScrollbar:{type:Boolean,required:!0},scrollbarProps:Object,trapFocus:{type:Boolean,default:!0},autoFocus:{type:Boolean,default:!0},showMask:{type:[Boolean,String],required:!0},maxWidth:Number,maxHeight:Number,minWidth:Number,minHeight:Number,resizable:Boolean,onClickoutside:Function,onAfterLeave:Function,onAfterEnter:Function,onEsc:Function},setup(e){const r=w(!!e.show),n=w(null),c=ze(ge);let b=0,C="",u=null;const v=w(!1),p=w(!1),y=M(()=>e.placement==="top"||e.placement==="bottom"),{mergedClsPrefixRef:k,mergedRtlRef:R}=xe(e),A=je("Drawer",R,k),P=a,h=i=>{p.value=!0,b=y.value?i.clientY:i.clientX,C=document.body.style.cursor,document.body.style.cursor=y.value?"ns-resize":"ew-resize",document.body.addEventListener("mousemove",F),document.body.addEventListener("mouseleave",P),document.body.addEventListener("mouseup",a)},N=()=>{u!==null&&(window.clearTimeout(u),u=null),p.value?v.value=!0:u=window.setTimeout(()=>{v.value=!0},300)},q=()=>{u!==null&&(window.clearTimeout(u),u=null),v.value=!1},{doUpdateHeight:K,doUpdateWidth:J}=c,D=i=>{const{maxWidth:l}=e;if(l&&i>l)return l;const{minWidth:f}=e;return f&&i<f?f:i},U=i=>{const{maxHeight:l}=e;if(l&&i>l)return l;const{minHeight:f}=e;return f&&i<f?f:i};function F(i){if(p.value)if(y.value){let l=n.value?.offsetHeight||0;const f=b-i.clientY;l+=e.placement==="bottom"?f:-f,l=U(l),K(l),b=i.clientY}else{let l=n.value?.offsetWidth||0;const f=b-i.clientX;l+=e.placement==="right"?f:-f,l=D(l),J(l),b=i.clientX}}function a(){p.value&&(b=0,p.value=!1,document.body.style.cursor=C,document.body.removeEventListener("mousemove",F),document.body.removeEventListener("mouseup",a),document.body.removeEventListener("mouseleave",P))}Xe(()=>{e.show&&(r.value=!0)}),Be(()=>e.show,i=>{i||a()}),Ye(()=>{a()});const m=M(()=>{const{show:i}=e,l=[[be,i]];return e.showMask||l.push([qe,e.onClickoutside,void 0,{capture:!0}]),l});function g(){r.value=!1,e.onAfterLeave?.()}return Ve(M(()=>e.blockScroll&&r.value)),te(Ke,n),te(Je,null),te(Qe,null),{bodyRef:n,rtlEnabled:A,mergedClsPrefix:c.mergedClsPrefixRef,isMounted:c.isMountedRef,mergedTheme:c.mergedThemeRef,displayed:r,transitionName:M(()=>({right:"slide-in-from-right-transition",left:"slide-in-from-left-transition",top:"slide-in-from-top-transition",bottom:"slide-in-from-bottom-transition"})[e.placement]),handleAfterLeave:g,bodyDirectives:m,handleMousedownResizeTrigger:h,handleMouseenterResizeTrigger:N,handleMouseleaveResizeTrigger:q,isDragging:p,isHoverOnResizeTrigger:v}},render(){const{$slots:e,mergedClsPrefix:r}=this;return this.displayDirective==="show"||this.displayed||this.show?he((d(),E("div",yt,[(d(),x(Ue,{disabled:!this.showMask||!this.trapFocus,active:this.show,autoFocus:this.autoFocus,onEsc:this.onEsc},{default:()=>(d(),x($e,{name:this.transitionName,appear:this.isMounted,onAfterEnter:this.onAfterEnter,onAfterLeave:this.handleAfterLeave},{default:()=>he(T("div",re(this.$attrs,{role:"dialog",ref:"bodyRef","aria-modal":"true",class:[`${r}-drawer`,this.rtlEnabled&&`${r}-drawer--rtl`,`${r}-drawer--${this.placement}-placement`,this.isDragging&&`${r}-drawer--unselectable`,this.nativeScrollbar&&`${r}-drawer--native-scrollbar`]}),[this.resizable?(d(),E("div",{key:2,class:B([`${r}-drawer__resize-trigger`,(this.isDragging||this.isHoverOnResizeTrigger)&&`${r}-drawer__resize-trigger--hover`]),onMouseenter:this.handleMouseenterResizeTrigger,onMouseleave:this.handleMouseleaveResizeTrigger,onMousedown:this.handleMousedownResizeTrigger},null,42,pt)):null,this.nativeScrollbar?(d(),E("div",{key:3,class:B([`${r}-drawer-content-wrapper`,this.contentClass]),style:V(this.contentStyle),role:"none"},[I(()=>e.default?.())],6)):(d(),x(ke,re({key:4},this.scrollbarProps,{contentStyle:this.contentStyle,contentClass:[`${r}-drawer-content-wrapper`,this.contentClass],theme:this.mergedTheme.peers.Scrollbar,themeOverrides:this.mergedTheme.peerOverrides.Scrollbar}),me(e),1040,["contentStyle","contentClass","theme","themeOverrides"]))]),this.bodyDirectives)},1032,["name","appear","onAfterEnter","onAfterLeave"]))},1032,["disabled","active","autoFocus","onEsc"]))])),[[be,this.displayDirective==="if"||this.displayed||this.show]]):null}});const{cubicBezierEaseIn:Ct,cubicBezierEaseOut:St}=oe;function $t({duration:e="0.3s",leaveDuration:r="0.2s",name:n="slide-in-from-bottom"}={}){return[s(`&.${n}-transition-leave-active`,{transition:`transform ${r} ${Ct}`}),s(`&.${n}-transition-enter-active`,{transition:`transform ${e} ${St}`}),s(`&.${n}-transition-enter-to`,{transform:"translateY(0)"}),s(`&.${n}-transition-enter-from`,{transform:"translateY(100%)"}),s(`&.${n}-transition-leave-from`,{transform:"translateY(0)"}),s(`&.${n}-transition-leave-to`,{transform:"translateY(100%)"})]}const{cubicBezierEaseIn:kt,cubicBezierEaseOut:zt}=oe;function xt({duration:e="0.3s",leaveDuration:r="0.2s",name:n="slide-in-from-left"}={}){return[s(`&.${n}-transition-leave-active`,{transition:`transform ${r} ${kt}`}),s(`&.${n}-transition-enter-active`,{transition:`transform ${e} ${zt}`}),s(`&.${n}-transition-enter-to`,{transform:"translateX(0)"}),s(`&.${n}-transition-enter-from`,{transform:"translateX(-100%)"}),s(`&.${n}-transition-leave-from`,{transform:"translateX(0)"}),s(`&.${n}-transition-leave-to`,{transform:"translateX(-100%)"})]}const{cubicBezierEaseIn:Bt,cubicBezierEaseOut:Et}=oe;function Rt({duration:e="0.3s",leaveDuration:r="0.2s",name:n="slide-in-from-right"}={}){return[s(`&.${n}-transition-leave-active`,{transition:`transform ${r} ${Bt}`}),s(`&.${n}-transition-enter-active`,{transition:`transform ${e} ${Et}`}),s(`&.${n}-transition-enter-to`,{transform:"translateX(0)"}),s(`&.${n}-transition-enter-from`,{transform:"translateX(100%)"}),s(`&.${n}-transition-leave-from`,{transform:"translateX(0)"}),s(`&.${n}-transition-leave-to`,{transform:"translateX(100%)"})]}const{cubicBezierEaseIn:_t,cubicBezierEaseOut:Tt}=oe;function Mt({duration:e="0.3s",leaveDuration:r="0.2s",name:n="slide-in-from-top"}={}){return[s(`&.${n}-transition-leave-active`,{transition:`transform ${r} ${_t}`}),s(`&.${n}-transition-enter-active`,{transition:`transform ${e} ${Tt}`}),s(`&.${n}-transition-enter-to`,{transform:"translateY(0)"}),s(`&.${n}-transition-enter-from`,{transform:"translateY(-100%)"}),s(`&.${n}-transition-leave-from`,{transform:"translateY(0)"}),s(`&.${n}-transition-leave-to`,{transform:"translateY(-100%)"})]}var Pt=s([z("drawer",`
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
 `),Ge({enterDuration:"0.2s",leaveDuration:"0.2s",enterCubicBezier:"var(--n-bezier-in)",leaveCubicBezier:"var(--n-bezier-out)"})])]);const Ft=["onClick"],Ht={...Ee.props,show:Boolean,width:[Number,String],height:[Number,String],placement:{type:String,default:"right"},maskClosable:{type:Boolean,default:!0},showMask:{type:[Boolean,String],default:!0},to:[String,Object],displayDirective:{type:String,default:"if"},nativeScrollbar:{type:Boolean,default:!0},zIndex:Number,onMaskClick:Function,scrollbarProps:Object,contentClass:String,contentStyle:[Object,String],trapFocus:{type:Boolean,default:!0},onEsc:Function,autoFocus:{type:Boolean,default:!0},closeOnEsc:{type:Boolean,default:!0},blockScroll:{type:Boolean,default:!0},maxWidth:Number,maxHeight:Number,minWidth:Number,minHeight:Number,resizable:Boolean,defaultWidth:{type:[Number,String],default:251},defaultHeight:{type:[Number,String],default:251},onUpdateWidth:[Function,Array],onUpdateHeight:[Function,Array],"onUpdate:width":[Function,Array],"onUpdate:height":[Function,Array],"onUpdate:show":[Function,Array],onUpdateShow:[Function,Array],onAfterEnter:Function,onAfterLeave:Function,drawerStyle:[String,Object],drawerClass:String,target:null,onShow:Function,onHide:Function};var Ot=ne({name:"Drawer",inheritAttrs:!1,props:Ht,setup(e){const{mergedClsPrefixRef:r,namespaceRef:n,inlineThemeDisabled:c}=xe(e),b=et(),C=Ee("Drawer","-drawer",Pt,ot,e,r),u=w(e.defaultWidth),v=w(e.defaultHeight),p=Ce(pe(e,"width"),u),y=Ce(pe(e,"height"),v),k=M(()=>{const{placement:a}=e;return a==="top"||a==="bottom"?"":we(p.value)}),R=M(()=>{const{placement:a}=e;return a==="left"||a==="right"?"":we(y.value)}),A=a=>{const{onUpdateWidth:m,"onUpdate:width":g}=e;m&&L(m,a),g&&L(g,a),u.value=a},P=a=>{const{onUpdateHeight:m,"onUpdate:width":g}=e;m&&L(m,a),g&&L(g,a),v.value=a},h=M(()=>[{width:k.value,height:R.value},e.drawerStyle||""]);function N(a){const{onMaskClick:m,maskClosable:g}=e;g&&D(!1),m&&m(a)}function q(a){N(a)}const K=tt();function J(a){e.onEsc?.(),e.show&&e.closeOnEsc&&nt(a)&&(K.value||D(!1))}function D(a){const{onHide:m,onUpdateShow:g,"onUpdate:show":i}=e;g&&L(g,a),i&&L(i,a),m&&!a&&L(m,a)}te(ge,{isMountedRef:b,mergedThemeRef:C,mergedClsPrefixRef:r,doUpdateShow:D,doUpdateHeight:P,doUpdateWidth:A});const U=M(()=>{const{common:{cubicBezierEaseInOut:a,cubicBezierEaseIn:m,cubicBezierEaseOut:g},self:{color:i,textColor:l,boxShadow:f,lineHeight:j,headerPadding:ae,footerPadding:se,borderRadius:ie,bodyPadding:le,titleFontSize:Z,titleTextColor:t,titleFontWeight:o,headerBorderBottom:H,footerBorderTop:X,closeIconColor:Y,closeIconColorHover:_e,closeIconColorPressed:Te,closeColorHover:Me,closeColorPressed:Pe,closeIconSize:Fe,closeSize:He,closeBorderRadius:Oe,resizableTriggerColorHover:Ie}}=C.value;return{"--n-line-height":j,"--n-color":i,"--n-border-radius":ie,"--n-text-color":l,"--n-box-shadow":f,"--n-bezier":a,"--n-bezier-out":g,"--n-bezier-in":m,"--n-header-padding":ae,"--n-body-padding":le,"--n-footer-padding":se,"--n-title-text-color":t,"--n-title-font-size":Z,"--n-title-font-weight":o,"--n-header-border-bottom":H,"--n-footer-border-top":X,"--n-close-icon-color":Y,"--n-close-icon-color-hover":_e,"--n-close-icon-color-pressed":Te,"--n-close-size":He,"--n-close-color-hover":Me,"--n-close-color-pressed":Pe,"--n-close-icon-size":Fe,"--n-close-border-radius":Oe,"--n-resize-trigger-color-hover":Ie}}),F=c?rt("drawer",void 0,U,e):void 0;return{mergedClsPrefix:r,namespace:n,mergedBodyStyle:h,handleOutsideClick:q,handleMaskClick:N,handleEsc:J,mergedTheme:C,cssVars:c?void 0:U,themeClass:F?.themeClass,onRender:F?.onRender,isMounted:b}},render(){const{mergedClsPrefix:e}=this;return d(),x(Ze,{to:this.to,show:this.show},{default:()=>(this.onRender?.(),he((d(),E("div",{class:B([`${e}-drawer-container`,this.namespace,this.themeClass]),style:V(this.cssVars),role:"none"},[this.showMask?(d(),x($e,{key:0,name:"fade-in-transition",appear:this.isMounted},{default:()=>this.show?(d(),E("div",{key:1,"aria-hidden":!0,class:B([`${e}-drawer-mask`,this.showMask==="transparent"&&`${e}-drawer-mask--invisible`]),onClick:this.handleMaskClick},null,10,Ft)):null},1032,["appear"])):I(()=>null),(d(),x(wt,re(this.$attrs,{class:[this.drawerClass,this.$attrs.class],style:[this.mergedBodyStyle,this.$attrs.style],blockScroll:this.blockScroll,contentStyle:this.contentStyle,contentClass:this.contentClass,placement:this.placement,scrollbarProps:this.scrollbarProps,show:this.show,displayDirective:this.displayDirective,nativeScrollbar:this.nativeScrollbar,onAfterEnter:this.onAfterEnter,onAfterLeave:this.onAfterLeave,trapFocus:this.trapFocus,autoFocus:this.autoFocus,resizable:this.resizable,maxHeight:this.maxHeight,minHeight:this.minHeight,maxWidth:this.maxWidth,minWidth:this.minWidth,showMask:this.showMask,onEsc:this.handleEsc,onClickoutside:this.handleOutsideClick}),me(this.$slots),1040,["class","style","blockScroll","contentStyle","contentClass","placement","scrollbarProps","show","displayDirective","nativeScrollbar","onAfterEnter","onAfterLeave","trapFocus","autoFocus","resizable","maxHeight","minHeight","maxWidth","minWidth","showMask","onEsc","onClickoutside"]))],6)),[[at,{zIndex:this.zIndex,enabled:this.show}]]))},1032,["to","show"])}});const It={title:String,headerClass:String,headerStyle:[Object,String],footerClass:String,footerStyle:[Object,String],bodyClass:String,bodyStyle:[Object,String],bodyContentClass:String,bodyContentStyle:[Object,String],nativeScrollbar:{type:Boolean,default:!0},scrollbarProps:Object,closable:Boolean};var At=ne({name:"DrawerContent",props:It,slots:Object,setup(){const e=ze(ge,null);e||it("drawer-content","`n-drawer-content` must be placed inside `n-drawer`.");const{doUpdateShow:r}=e;function n(){r(!1)}return{handleCloseClick:n,mergedTheme:e.mergedThemeRef,mergedClsPrefix:e.mergedClsPrefixRef}},render(){const{title:e,mergedClsPrefix:r,nativeScrollbar:n,mergedTheme:c,bodyClass:b,bodyStyle:C,bodyContentClass:u,bodyContentStyle:v,headerClass:p,headerStyle:y,footerClass:k,footerStyle:R,scrollbarProps:A,closable:P,$slots:h}=this;return d(),E("div",{role:"none",class:B([`${r}-drawer-content`,n&&`${r}-drawer-content--native-scrollbar`])},[h.header||e||P?(d(),E("div",{key:0,class:B([`${r}-drawer-header`,p]),style:V(y),role:"none"},[G("div",{class:B(`${r}-drawer-header__main`),role:"heading","aria-level":"1"},[h.header!==void 0?(d(),E(ye,{key:0},[I(()=>h.header())],64)):(d(),E(ye,{key:1},[I(()=>e)],64))],2),I(()=>P&&(d(),x(st,{onClick:this.handleCloseClick,clsPrefix:r,class:B(`${r}-drawer-header__close`),absolute:!0},null,8,["onClick","clsPrefix","class"])))],6)):I(()=>null),n?(d(),E("div",{key:2,class:B([`${r}-drawer-body`,b]),style:V(C),role:"none"},[G("div",{class:B([`${r}-drawer-body-content-wrapper`,u]),style:V(v),role:"none"},[I(()=>h.default?.())],6)],6)):(d(),x(ke,re({key:3,themeOverrides:c.peerOverrides.Scrollbar,theme:c.peers.Scrollbar},A,{class:`${r}-drawer-body`,contentClass:[`${r}-drawer-body-content-wrapper`,u],contentStyle:v}),me(h),1040,["themeOverrides","theme","class","contentClass","contentStyle"])),h.footer?(d(),E("div",{key:4,class:B([`${r}-drawer-footer`,k]),style:V(R),role:"none"},[I(()=>h.footer())],6)):I(()=>null)],2)}});async function Dt(e){return(await Re.get(`/servers/${e}/containers`)).data.containers??[]}async function ve(e,r,n){return(await Re.post(`/servers/${e}/containers/${encodeURIComponent(r)}/${n}`,{})).data.container_id}function Wt(e,r){return ve(e,r,"start")}function Lt(e,r){return ve(e,r,"stop")}function Nt(e,r){return ve(e,r,"restart")}function Se(e){return De(e)?e.message||"Request failed":e instanceof Error?e.message:"Something went wrong. Please try again."}const Ut={class:"breadcrumb","aria-label":"Breadcrumb"},jt={class:"muted"},Xt=5e3,Yt=ne({__name:"ContainersPage",setup(e){const r=ft(),n=mt(),c=M(()=>String(r.params.id??"")),b=w([]),C=w(!1),u=w(null),v=w(""),p=w(!1),y=w({}),k=w(null),R=w(!1),A=gt("(max-width: 760px)"),P=M(()=>A.value?"94vw":720);let h=null;function N(t){switch(t.trim().toLowerCase()){case"running":return"success";case"restarting":case"paused":case"created":return"warning";case"exited":case"dead":return"error";default:return"default"}}function q(t){return t.trim().toLowerCase()==="running"}function K(t,o){return y.value[`${t}:${o}`]===!0}function J(t){return Object.keys(y.value).some(o=>o.startsWith(`${t}:`))}function D(t){const o=t.status||t.state||"unknown";return T("span",{title:o},[T(Le,{type:N(t.state),size:"small",round:!0},{default:()=>t.state||"unknown"})])}function U(t){return!t.ports||t.ports.length===0?T(fe,{depth:3},{default:()=>"—"}):T("span",{class:"mono"},t.ports.join(", "))}function F(t,o,H){return T(ue,{size:"small",secondary:!0,loading:K(t.id,o),disabled:J(t.id),onClick:X=>{X.stopPropagation(),se(t,o,H)}},{default:()=>H})}function a(t){return T(ue,{size:"small",quaternary:!0,onClick:o=>{o.stopPropagation(),f(t)}},{default:()=>"Logs"})}function m(t){const o=[];return q(t.state)?(o.push(F(t,"restart","Restart")),o.push(F(t,"stop","Stop"))):o.push(F(t,"start","Start")),o.push(a(t)),T(ee,{size:8,align:"center",wrap:!1},{default:()=>o})}const g=[{title:"Name",key:"name",minWidth:180,ellipsis:{tooltip:!0},render:t=>T("span",{class:"mono"},t.name||t.id)},{title:"Image",key:"image",minWidth:180,ellipsis:{tooltip:!0},render:t=>T("span",{class:"mono muted"},t.image||"—")},{title:"State",key:"state",width:140,render:t=>D(t)},{title:"Ports",key:"ports",minWidth:140,render:t=>U(t)},{title:"Actions",key:"actions",width:220,render:t=>m(t)}];function i(t){return t.id}function l(t){return{style:"cursor: pointer;",onClick:()=>f(t)}}function f(t){k.value=t,R.value=!0}async function j(t){if(c.value){t&&(C.value=!0);try{b.value=await Dt(c.value),u.value=null,p.value=!0}catch(o){u.value=Se(o)}finally{C.value=!1}}}async function ae(){if(c.value)try{const t=await We(c.value);v.value=t.name}catch{}}async function se(t,o,H){const X=`${t.id}:${o}`;y.value={...y.value,[X]:!0};try{o==="start"?await Wt(c.value,t.id):o==="stop"?await Lt(c.value,t.id):await Nt(c.value,t.id),n.success(`${H} requested for ${t.name}`),await j(!1)}catch(Y){n.error(Se(Y))}finally{const Y={...y.value};delete Y[X],y.value=Y}}function ie(){h===null&&(h=setInterval(()=>{j(!1)},Xt))}function le(){h!==null&&(clearInterval(h),h=null)}async function Z(){p.value=!1,await Promise.all([ae(),j(!0)])}return Be(c,()=>{k.value=null,R.value=!1,Z()}),lt(()=>{Z(),ie()}),dt(()=>{le()}),(t,o)=>(d(),x($(ee),{vertical:"",size:16},{default:S(()=>[G("nav",Ut,[_($(ct),{to:"/servers"},{default:S(()=>[...o[2]||(o[2]=[Q("Servers",-1)])]),_:1}),o[3]||(o[3]=G("span",{class:"breadcrumb__sep"},"/",-1)),G("span",jt,de(v.value||c.value),1)]),_($(ut),null,{header:S(()=>[_($(ee),{align:"center",justify:"space-between"},{default:S(()=>[_($(ee),{align:"center",size:10},{default:S(()=>[_($(fe),{strong:""},{default:S(()=>[...o[4]||(o[4]=[Q("Containers",-1)])]),_:1}),v.value?(d(),x($(fe),{key:0,depth:"3"},{default:S(()=>[Q(de(v.value),1)]),_:1})):ce("",!0)]),_:1}),_($(ue),{secondary:"",loading:C.value,onClick:o[0]||(o[0]=H=>j(!0))},{default:S(()=>[...o[5]||(o[5]=[Q(" Refresh ",-1)])]),_:1},8,["loading"])]),_:1})]),default:S(()=>[u.value?(d(),x($(Ne),{key:0,type:"error","show-icon":!0,style:{"margin-bottom":"12px"}},{default:S(()=>[Q(de(u.value),1)]),_:1})):ce("",!0),_($(ht),{columns:g,data:b.value,loading:C.value,"row-key":i,"row-props":l,bordered:!1,"scroll-x":960,pagination:{pageSize:10}},{empty:S(()=>[_($(Ae),{description:p.value?"No containers on this node.":"Loading containers…"},null,8,["description"])]),_:1},8,["data","loading"])]),_:1}),_($(Ot),{show:R.value,"onUpdate:show":o[1]||(o[1]=H=>R.value=H),width:P.value,placement:"right"},{default:S(()=>[_($(At),{closable:"","native-scrollbar":!1},{default:S(()=>[k.value?(d(),x(vt,{key:0,"server-id":c.value,"container-id":k.value.id,title:k.value.name,subtitle:k.value.image},null,8,["server-id","container-id","title","subtitle"])):ce("",!0)]),_:1})]),_:1},8,["show","width"])]),_:1}))}}),gr=bt(Yt,[["__scopeId","data-v-5ceaa3bb"]]);export{gr as default};
