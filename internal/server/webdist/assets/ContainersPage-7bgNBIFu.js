import{E as He}from"./Empty-BdvWVmVU.js";import{T as Ne}from"./Tag-BJs1p51-.js";import{A as Ae}from"./Alert-B4-gAdn-.js";import{I as We}from"./Input-B46U3-dY.js";import{d as se,a6 as Ce,a7 as $e,o as d,c as H,m as T,a8 as Re,M as R,W as O,V as Q,O as W,a9 as Te,N as ae,a2 as Se,aa as De,q as p,ab as Me,ac as ke,P as Fe,ad as Le,ae as Ue,D as Pe,E as je,af as Xe,y as M,ag as Ye,ah as oe,ai as Ve,aj as qe,ak as Ke,J as s,al as ie,K as _,a1 as A,am as U,an as Ge,L as Ie,ao as Qe,ap as Je,aq as Ze,a3 as et,ar as tt,as as j,at as rt,au as nt,av as ze,a as B,F as xe,aw as ot,ax as at,g as st,A as it,w as y,u as w,e as x,k as D,j as lt,t as G,l as pe,x as be,B as ye,C as dt,h as ut}from"./index-5CpOptKS.js";import{N as ct}from"./Icon-xWmKUS9A.js";import{D as ft}from"./DataTable-DgQUCzmj.js";import{u as ht}from"./use-message-CCY2xjhw.js";import{f as Be}from"./format-length-Da-Ct1Zi.js";import{u as Ee}from"./use-merged-state-BwH_rwWJ.js";import{S as ne}from"./Space-C6oaRjJg.js";import{t as we}from"./text-UsFW0JYy.js";import{l as mt,d as _e,L as vt,s as gt,a as pt,r as bt}from"./LogViewer-BfjN_FmX.js";import{g as yt}from"./servers-D1YW7eHb.js";import{u as wt}from"./useMediaQuery-BCQSu8N0.js";import{G as Ct}from"./GothamIcon-YLFnCcPQ.js";import{_ as St}from"./_plugin-vue_export-helper-DlAUqK2U.js";import"./Pagination-DwCYpkiU.js";import"./Popover-B2pxAPuK.js";import"./create-ref-setter-C4J8sofl.js";import"./Select-C3gX7jcs.js";import"./create-DLefnRiD.js";import"./CheckboxGroup-3p4wIir2.js";import"./Tooltip-BsD7vKz9.js";import"./RadioGroup-Ckq_T8f4.js";import"./Dropdown-CD71Nigs.js";import"./ChevronRight-C34Kk_U-.js";const kt=["onMouseenter","onMouseleave","onMousedown"],$t={key:1,role:"none"};var zt=se({name:"NDrawerContent",inheritAttrs:!1,props:{blockScroll:Boolean,show:{type:Boolean,default:void 0},displayDirective:{type:String,required:!0},placement:{type:String,required:!0},contentClass:String,contentStyle:[Object,String],nativeScrollbar:{type:Boolean,required:!0},scrollbarProps:Object,trapFocus:{type:Boolean,default:!0},autoFocus:{type:Boolean,default:!0},showMask:{type:[Boolean,String],required:!0},maxWidth:Number,maxHeight:Number,minWidth:Number,minHeight:Number,resizable:Boolean,onClickoutside:Function,onAfterLeave:Function,onAfterEnter:Function,onEsc:Function},setup(t){const n=p(!!t.show),o=p(null),u=Me(ke);let c=0,$="",f=null;const b=p(!1),C=p(!1),S=M(()=>t.placement==="top"||t.placement==="bottom"),{mergedClsPrefixRef:E,mergedRtlRef:N}=Fe(t),v=Le("Drawer",N,E),F=a,z=i=>{C.value=!0,c=S.value?i.clientY:i.clientX,$=document.body.style.cursor,document.body.style.cursor=S.value?"ns-resize":"ew-resize",document.body.addEventListener("mousemove",I),document.body.addEventListener("mouseleave",F),document.body.addEventListener("mouseup",a)},X=()=>{f!==null&&(window.clearTimeout(f),f=null),C.value?b.value=!0:f=window.setTimeout(()=>{b.value=!0},300)},L=()=>{f!==null&&(window.clearTimeout(f),f=null),b.value=!1},{doUpdateHeight:J,doUpdateWidth:Z}=u,P=i=>{const{maxWidth:l}=t;if(l&&i>l)return l;const{minWidth:m}=t;return m&&i<m?m:i},Y=i=>{const{maxHeight:l}=t;if(l&&i>l)return l;const{minHeight:m}=t;return m&&i<m?m:i};function I(i){if(C.value)if(S.value){let l=o.value?.offsetHeight||0;const m=c-i.clientY;l+=t.placement==="bottom"?m:-m,l=Y(l),J(l),c=i.clientY}else{let l=o.value?.offsetWidth||0;const m=c-i.clientX;l+=t.placement==="right"?m:-m,l=P(l),Z(l),c=i.clientX}}function a(){C.value&&(c=0,C.value=!1,document.body.style.cursor=$,document.body.removeEventListener("mousemove",I),document.body.removeEventListener("mouseup",a),document.body.removeEventListener("mouseleave",F))}Ue(()=>{t.show&&(n.value=!0)}),Pe(()=>t.show,i=>{i||a()}),je(()=>{a()});const h=M(()=>{const{show:i}=t,l=[[$e,i]];return t.showMask||l.push([Ye,t.onClickoutside,void 0,{capture:!0}]),l});function g(){n.value=!1,t.onAfterLeave?.()}return Xe(M(()=>t.blockScroll&&n.value)),oe(Ve,o),oe(qe,null),oe(Ke,null),{bodyRef:o,rtlEnabled:v,mergedClsPrefix:u.mergedClsPrefixRef,isMounted:u.isMountedRef,mergedTheme:u.mergedThemeRef,displayed:n,transitionName:M(()=>({right:"slide-in-from-right-transition",left:"slide-in-from-left-transition",top:"slide-in-from-top-transition",bottom:"slide-in-from-bottom-transition"})[t.placement]),handleAfterLeave:g,bodyDirectives:h,handleMousedownResizeTrigger:z,handleMouseenterResizeTrigger:X,handleMouseleaveResizeTrigger:L,isDragging:C,isHoverOnResizeTrigger:b}},render(){const{$slots:t,mergedClsPrefix:n}=this;return this.displayDirective==="show"||this.displayed||this.show?Ce((d(),H("div",$t,[(d(),T(De,{disabled:!this.showMask||!this.trapFocus,active:this.show,autoFocus:this.autoFocus,onEsc:this.onEsc},{default:()=>(d(),T(Re,{name:this.transitionName,appear:this.isMounted,onAfterEnter:this.onAfterEnter,onAfterLeave:this.handleAfterLeave},{default:()=>Ce(R("div",ae(this.$attrs,{role:"dialog",ref:"bodyRef","aria-modal":"true",class:[`${n}-drawer`,this.rtlEnabled&&`${n}-drawer--rtl`,`${n}-drawer--${this.placement}-placement`,this.isDragging&&`${n}-drawer--unselectable`,this.nativeScrollbar&&`${n}-drawer--native-scrollbar`]}),[this.resizable?(d(),H("div",{key:2,class:O([`${n}-drawer__resize-trigger`,(this.isDragging||this.isHoverOnResizeTrigger)&&`${n}-drawer__resize-trigger--hover`]),onMouseenter:this.handleMouseenterResizeTrigger,onMouseleave:this.handleMouseleaveResizeTrigger,onMousedown:this.handleMousedownResizeTrigger},null,42,kt)):null,this.nativeScrollbar?(d(),H("div",{key:3,class:O([`${n}-drawer-content-wrapper`,this.contentClass]),style:Q(this.contentStyle),role:"none"},[W(()=>t.default?.())],6)):(d(),T(Te,ae({key:4},this.scrollbarProps,{contentStyle:this.contentStyle,contentClass:[`${n}-drawer-content-wrapper`,this.contentClass],theme:this.mergedTheme.peers.Scrollbar,themeOverrides:this.mergedTheme.peerOverrides.Scrollbar}),Se(t),1040,["contentStyle","contentClass","theme","themeOverrides"]))]),this.bodyDirectives)},1032,["name","appear","onAfterEnter","onAfterLeave"]))},1032,["disabled","active","autoFocus","onEsc"]))])),[[$e,this.displayDirective==="if"||this.displayed||this.show]]):null}});const{cubicBezierEaseIn:xt,cubicBezierEaseOut:Bt}=ie;function Et({duration:t="0.3s",leaveDuration:n="0.2s",name:o="slide-in-from-bottom"}={}){return[s(`&.${o}-transition-leave-active`,{transition:`transform ${n} ${xt}`}),s(`&.${o}-transition-enter-active`,{transition:`transform ${t} ${Bt}`}),s(`&.${o}-transition-enter-to`,{transform:"translateY(0)"}),s(`&.${o}-transition-enter-from`,{transform:"translateY(100%)"}),s(`&.${o}-transition-leave-from`,{transform:"translateY(0)"}),s(`&.${o}-transition-leave-to`,{transform:"translateY(100%)"})]}const{cubicBezierEaseIn:_t,cubicBezierEaseOut:Rt}=ie;function Tt({duration:t="0.3s",leaveDuration:n="0.2s",name:o="slide-in-from-left"}={}){return[s(`&.${o}-transition-leave-active`,{transition:`transform ${n} ${_t}`}),s(`&.${o}-transition-enter-active`,{transition:`transform ${t} ${Rt}`}),s(`&.${o}-transition-enter-to`,{transform:"translateX(0)"}),s(`&.${o}-transition-enter-from`,{transform:"translateX(-100%)"}),s(`&.${o}-transition-leave-from`,{transform:"translateX(0)"}),s(`&.${o}-transition-leave-to`,{transform:"translateX(-100%)"})]}const{cubicBezierEaseIn:Mt,cubicBezierEaseOut:Ft}=ie;function Pt({duration:t="0.3s",leaveDuration:n="0.2s",name:o="slide-in-from-right"}={}){return[s(`&.${o}-transition-leave-active`,{transition:`transform ${n} ${Mt}`}),s(`&.${o}-transition-enter-active`,{transition:`transform ${t} ${Ft}`}),s(`&.${o}-transition-enter-to`,{transform:"translateX(0)"}),s(`&.${o}-transition-enter-from`,{transform:"translateX(100%)"}),s(`&.${o}-transition-leave-from`,{transform:"translateX(0)"}),s(`&.${o}-transition-leave-to`,{transform:"translateX(100%)"})]}const{cubicBezierEaseIn:It,cubicBezierEaseOut:Ot}=ie;function Ht({duration:t="0.3s",leaveDuration:n="0.2s",name:o="slide-in-from-top"}={}){return[s(`&.${o}-transition-leave-active`,{transition:`transform ${n} ${It}`}),s(`&.${o}-transition-enter-active`,{transition:`transform ${t} ${Ot}`}),s(`&.${o}-transition-enter-to`,{transform:"translateY(0)"}),s(`&.${o}-transition-enter-from`,{transform:"translateY(-100%)"}),s(`&.${o}-transition-leave-from`,{transform:"translateY(0)"}),s(`&.${o}-transition-leave-to`,{transform:"translateY(-100%)"})]}var Nt=s([_("drawer",`
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
 `,[Pt(),Tt(),Ht(),Et(),A("unselectable",`
 user-select: none; 
 -webkit-user-select: none;
 `),A("native-scrollbar",[_("drawer-content-wrapper",`
 overflow: auto;
 height: 100%;
 `)]),U("resize-trigger",`
 position: absolute;
 background-color: #0000;
 transition: background-color .3s var(--n-bezier);
 `,[A("hover",`
 background-color: var(--n-resize-trigger-color-hover);
 `)]),_("drawer-content-wrapper",`
 box-sizing: border-box;
 `),_("drawer-content",`
 height: 100%;
 display: flex;
 flex-direction: column;
 `,[A("native-scrollbar",[_("drawer-body-content-wrapper",`
 height: 100%;
 overflow: auto;
 `)]),_("drawer-body",`
 flex: 1 0 0;
 overflow: hidden;
 `),_("drawer-body-content-wrapper",`
 box-sizing: border-box;
 padding: var(--n-body-padding);
 `),_("drawer-header",`
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
 `,[U("main",`
 flex: 1;
 `),U("close",`
 margin-left: 6px;
 transition:
 background-color .3s var(--n-bezier),
 color .3s var(--n-bezier);
 `)]),_("drawer-footer",`
 display: flex;
 justify-content: flex-end;
 border-top: var(--n-footer-border-top);
 transition: border .3s var(--n-bezier);
 padding: var(--n-footer-padding);
 `)]),A("right-placement",`
 top: 0;
 bottom: 0;
 right: 0;
 border-top-left-radius: var(--n-border-radius);
 border-bottom-left-radius: var(--n-border-radius);
 `,[U("resize-trigger",`
 width: 3px;
 height: 100%;
 top: 0;
 left: 0;
 transform: translateX(-1.5px);
 cursor: ew-resize;
 `)]),A("left-placement",`
 top: 0;
 bottom: 0;
 left: 0;
 border-top-right-radius: var(--n-border-radius);
 border-bottom-right-radius: var(--n-border-radius);
 `,[U("resize-trigger",`
 width: 3px;
 height: 100%;
 top: 0;
 right: 0;
 transform: translateX(1.5px);
 cursor: ew-resize;
 `)]),A("top-placement",`
 top: 0;
 left: 0;
 right: 0;
 border-bottom-left-radius: var(--n-border-radius);
 border-bottom-right-radius: var(--n-border-radius);
 `,[U("resize-trigger",`
 width: 100%;
 height: 3px;
 bottom: 0;
 left: 0;
 transform: translateY(1.5px);
 cursor: ns-resize;
 `)]),A("bottom-placement",`
 left: 0;
 bottom: 0;
 right: 0;
 border-top-left-radius: var(--n-border-radius);
 border-top-right-radius: var(--n-border-radius);
 `,[U("resize-trigger",`
 width: 100%;
 height: 3px;
 top: 0;
 left: 0;
 transform: translateY(-1.5px);
 cursor: ns-resize;
 `)])]),s("body",[s(">",[_("drawer-container",`
 position: fixed;
 `)])]),_("drawer-container",`
 position: relative;
 position: absolute;
 left: 0;
 right: 0;
 top: 0;
 bottom: 0;
 pointer-events: none;
 `,[s("> *",`
 pointer-events: all;
 `)]),_("drawer-mask",`
 background-color: rgba(0, 0, 0, .3);
 position: absolute;
 left: 0;
 right: 0;
 top: 0;
 bottom: 0;
 `,[A("invisible",`
 background-color: rgba(0, 0, 0, 0)
 `),Ge({enterDuration:"0.2s",leaveDuration:"0.2s",enterCubicBezier:"var(--n-bezier-in)",leaveCubicBezier:"var(--n-bezier-out)"})])]);const At=["onClick"],Wt={...Ie.props,show:Boolean,width:[Number,String],height:[Number,String],placement:{type:String,default:"right"},maskClosable:{type:Boolean,default:!0},showMask:{type:[Boolean,String],default:!0},to:[String,Object],displayDirective:{type:String,default:"if"},nativeScrollbar:{type:Boolean,default:!0},zIndex:Number,onMaskClick:Function,scrollbarProps:Object,contentClass:String,contentStyle:[Object,String],trapFocus:{type:Boolean,default:!0},onEsc:Function,autoFocus:{type:Boolean,default:!0},closeOnEsc:{type:Boolean,default:!0},blockScroll:{type:Boolean,default:!0},maxWidth:Number,maxHeight:Number,minWidth:Number,minHeight:Number,resizable:Boolean,defaultWidth:{type:[Number,String],default:251},defaultHeight:{type:[Number,String],default:251},onUpdateWidth:[Function,Array],onUpdateHeight:[Function,Array],"onUpdate:width":[Function,Array],"onUpdate:height":[Function,Array],"onUpdate:show":[Function,Array],onUpdateShow:[Function,Array],onAfterEnter:Function,onAfterLeave:Function,drawerStyle:[String,Object],drawerClass:String,target:null,onShow:Function,onHide:Function};var Dt=se({name:"Drawer",inheritAttrs:!1,props:Wt,setup(t){const{mergedClsPrefixRef:n,namespaceRef:o,inlineThemeDisabled:u}=Fe(t),c=Je(),$=Ie("Drawer","-drawer",Nt,rt,t,n),f=p(t.defaultWidth),b=p(t.defaultHeight),C=Ee(ze(t,"width"),f),S=Ee(ze(t,"height"),b),E=M(()=>{const{placement:a}=t;return a==="top"||a==="bottom"?"":Be(C.value)}),N=M(()=>{const{placement:a}=t;return a==="left"||a==="right"?"":Be(S.value)}),v=a=>{const{onUpdateWidth:h,"onUpdate:width":g}=t;h&&j(h,a),g&&j(g,a),f.value=a},F=a=>{const{onUpdateHeight:h,"onUpdate:width":g}=t;h&&j(h,a),g&&j(g,a),b.value=a},z=M(()=>[{width:E.value,height:N.value},t.drawerStyle||""]);function X(a){const{onMaskClick:h,maskClosable:g}=t;g&&P(!1),h&&h(a)}function L(a){X(a)}const J=Ze();function Z(a){t.onEsc?.(),t.show&&t.closeOnEsc&&tt(a)&&(J.value||P(!1))}function P(a){const{onHide:h,onUpdateShow:g,"onUpdate:show":i}=t;g&&j(g,a),i&&j(i,a),h&&!a&&j(h,a)}oe(ke,{isMountedRef:c,mergedThemeRef:$,mergedClsPrefixRef:n,doUpdateShow:P,doUpdateHeight:F,doUpdateWidth:v});const Y=M(()=>{const{common:{cubicBezierEaseInOut:a,cubicBezierEaseIn:h,cubicBezierEaseOut:g},self:{color:i,textColor:l,boxShadow:m,lineHeight:ee,headerPadding:le,footerPadding:de,borderRadius:ue,bodyPadding:ce,titleFontSize:fe,titleTextColor:te,titleFontWeight:V,headerBorderBottom:he,footerBorderTop:me,closeIconColor:ve,closeIconColorHover:ge,closeIconColorPressed:re,closeColorHover:e,closeColorPressed:r,closeIconSize:k,closeSize:q,closeBorderRadius:K,resizableTriggerColorHover:Oe}}=$.value;return{"--n-line-height":ee,"--n-color":i,"--n-border-radius":ue,"--n-text-color":l,"--n-box-shadow":m,"--n-bezier":a,"--n-bezier-out":g,"--n-bezier-in":h,"--n-header-padding":le,"--n-body-padding":ce,"--n-footer-padding":de,"--n-title-text-color":te,"--n-title-font-size":fe,"--n-title-font-weight":V,"--n-header-border-bottom":he,"--n-footer-border-top":me,"--n-close-icon-color":ve,"--n-close-icon-color-hover":ge,"--n-close-icon-color-pressed":re,"--n-close-size":q,"--n-close-color-hover":e,"--n-close-color-pressed":r,"--n-close-icon-size":k,"--n-close-border-radius":K,"--n-resize-trigger-color-hover":Oe}}),I=u?et("drawer",void 0,Y,t):void 0;return{mergedClsPrefix:n,namespace:o,mergedBodyStyle:z,handleOutsideClick:L,handleMaskClick:X,handleEsc:Z,mergedTheme:$,cssVars:u?void 0:Y,themeClass:I?.themeClass,onRender:I?.onRender,isMounted:c}},render(){const{mergedClsPrefix:t}=this;return d(),T(Qe,{to:this.to,show:this.show},{default:()=>(this.onRender?.(),Ce((d(),H("div",{class:O([`${t}-drawer-container`,this.namespace,this.themeClass]),style:Q(this.cssVars),role:"none"},[this.showMask?(d(),T(Re,{key:0,name:"fade-in-transition",appear:this.isMounted},{default:()=>this.show?(d(),H("div",{key:1,"aria-hidden":!0,class:O([`${t}-drawer-mask`,this.showMask==="transparent"&&`${t}-drawer-mask--invisible`]),onClick:this.handleMaskClick},null,10,At)):null},1032,["appear"])):W(()=>null),(d(),T(zt,ae(this.$attrs,{class:[this.drawerClass,this.$attrs.class],style:[this.mergedBodyStyle,this.$attrs.style],blockScroll:this.blockScroll,contentStyle:this.contentStyle,contentClass:this.contentClass,placement:this.placement,scrollbarProps:this.scrollbarProps,show:this.show,displayDirective:this.displayDirective,nativeScrollbar:this.nativeScrollbar,onAfterEnter:this.onAfterEnter,onAfterLeave:this.onAfterLeave,trapFocus:this.trapFocus,autoFocus:this.autoFocus,resizable:this.resizable,maxHeight:this.maxHeight,minHeight:this.minHeight,maxWidth:this.maxWidth,minWidth:this.minWidth,showMask:this.showMask,onEsc:this.handleEsc,onClickoutside:this.handleOutsideClick}),Se(this.$slots),1040,["class","style","blockScroll","contentStyle","contentClass","placement","scrollbarProps","show","displayDirective","nativeScrollbar","onAfterEnter","onAfterLeave","trapFocus","autoFocus","resizable","maxHeight","minHeight","maxWidth","minWidth","showMask","onEsc","onClickoutside"]))],6)),[[nt,{zIndex:this.zIndex,enabled:this.show}]]))},1032,["to","show"])}});const Lt={title:String,headerClass:String,headerStyle:[Object,String],footerClass:String,footerStyle:[Object,String],bodyClass:String,bodyStyle:[Object,String],bodyContentClass:String,bodyContentStyle:[Object,String],nativeScrollbar:{type:Boolean,default:!0},scrollbarProps:Object,closable:Boolean};var Ut=se({name:"DrawerContent",props:Lt,slots:Object,setup(){const t=Me(ke,null);t||at("drawer-content","`n-drawer-content` must be placed inside `n-drawer`.");const{doUpdateShow:n}=t;function o(){n(!1)}return{handleCloseClick:o,mergedTheme:t.mergedThemeRef,mergedClsPrefix:t.mergedClsPrefixRef}},render(){const{title:t,mergedClsPrefix:n,nativeScrollbar:o,mergedTheme:u,bodyClass:c,bodyStyle:$,bodyContentClass:f,bodyContentStyle:b,headerClass:C,headerStyle:S,footerClass:E,footerStyle:N,scrollbarProps:v,closable:F,$slots:z}=this;return d(),H("div",{role:"none",class:O([`${n}-drawer-content`,o&&`${n}-drawer-content--native-scrollbar`])},[z.header||t||F?(d(),H("div",{key:0,class:O([`${n}-drawer-header`,C]),style:Q(S),role:"none"},[B("div",{class:O(`${n}-drawer-header__main`),role:"heading","aria-level":"1"},[z.header!==void 0?(d(),H(xe,{key:0},[W(()=>z.header())],64)):(d(),H(xe,{key:1},[W(()=>t)],64))],2),W(()=>F&&(d(),T(ot,{onClick:this.handleCloseClick,clsPrefix:n,class:O(`${n}-drawer-header__close`),absolute:!0},null,8,["onClick","clsPrefix","class"])))],6)):W(()=>null),o?(d(),H("div",{key:2,class:O([`${n}-drawer-body`,c]),style:Q($),role:"none"},[B("div",{class:O([`${n}-drawer-body-content-wrapper`,f]),style:Q(b),role:"none"},[W(()=>z.default?.())],6)],6)):(d(),T(Te,ae({key:3,themeOverrides:u.peerOverrides.Scrollbar,theme:u.peers.Scrollbar},v,{class:`${n}-drawer-body`,contentClass:[`${n}-drawer-body-content-wrapper`,f],contentStyle:b}),Se(z),1040,["themeOverrides","theme","class","contentClass","contentStyle"])),z.footer?(d(),H("div",{key:4,class:O([`${n}-drawer-footer`,E]),style:Q(N),role:"none"},[W(()=>z.footer())],6)):W(()=>null)],2)}});const jt={class:"breadcrumb","aria-label":"Breadcrumb"},Xt={class:"muted","aria-current":"page"},Yt={class:"toolbar"},Vt={class:"filters",role:"group","aria-label":"Filter containers by status"},qt=["aria-pressed"],Kt={class:"nav-count"},Gt=["aria-pressed"],Qt={class:"nav-count"},Jt=["aria-pressed"],Zt={class:"nav-count"},er=5e3,tr=se({__name:"ContainersPage",setup(t){const n=ut(),o=ht(),u=M(()=>String(n.params.id??"")),c=p([]),$=p(!1),f=p(null),b=p(""),C=p(!1),S=p({}),E=p(null),N=p(!1),v=p("all"),F=p("");function z(e,r){switch(r){case"running":return I(e.state);case"exited":return!I(e.state);default:return!0}}const X=M(()=>{const e=F.value.trim().toLowerCase();return c.value.filter(r=>z(r,v.value)?e===""?!0:`${r.name} ${r.image}`.toLowerCase().includes(e):!1)}),L=M(()=>({all:c.value.length,running:c.value.filter(e=>I(e.state)).length,exited:c.value.filter(e=>!I(e.state)).length})),J=wt("(max-width: 760px)"),Z=M(()=>J.value?"94vw":720);let P=null;function Y(e){switch(e.trim().toLowerCase()){case"running":return"success";case"restarting":case"paused":case"created":return"warning";case"exited":case"dead":return"error";default:return"default"}}function I(e){return e.trim().toLowerCase()==="running"}function a(e,r){return S.value[`${e}:${r}`]===!0}function h(e){return Object.keys(S.value).some(r=>r.startsWith(`${e}:`))}function g(e){const r=e.status||e.state||"unknown";return R("span",{title:r},[R(Ne,{type:Y(e.state),size:"small",round:!0},{default:()=>e.state||"unknown"})])}function i(e){return!e.ports||e.ports.length===0?R(we,{depth:3},{default:()=>"—"}):R("span",{class:"mono"},e.ports.join(", "))}function l(){return R("span",{class:"mono muted",title:"Not reported by the agent yet","aria-label":"Not reported by the agent yet"},"—")}function m(e){return R("span",{class:"mono muted"},e.status||"—")}function ee(e,r,k){return R(ye,{size:"small",secondary:!0,loading:a(e.id,r),disabled:h(e.id),onClick:q=>{q.stopPropagation(),me(e,r,k)}},{default:()=>k})}function le(e){return R(ye,{size:"small",quaternary:!0,onClick:r=>{r.stopPropagation(),te(e)}},{default:()=>"Logs"})}function de(e){const r=[];return I(e.state)?(r.push(ee(e,"restart","Restart")),r.push(ee(e,"stop","Stop"))):r.push(ee(e,"start","Start")),r.push(le(e)),R(ne,{size:8,align:"center",wrap:!1},{default:()=>r})}const ue=[{title:"Name",key:"name",minWidth:180,ellipsis:{tooltip:!0},render:e=>R("span",{class:"mono"},e.name||e.id)},{title:"Image",key:"image",minWidth:180,ellipsis:{tooltip:!0},render:e=>R("span",{class:"mono muted"},e.image||"—")},{title:"State",key:"state",width:140,render:e=>g(e)},{title:"Ports",key:"ports",minWidth:140,render:e=>i(e)},{title:"CPU",key:"cpu",width:80,render:()=>l()},{title:"RAM",key:"ram",width:90,render:()=>l()},{title:"Uptime",key:"uptime",minWidth:140,ellipsis:{tooltip:!0},render:e=>m(e)},{title:"Actions",key:"actions",width:220,render:e=>de(e)}];function ce(e){return e.id}function fe(e){return{style:"cursor: pointer;",onClick:()=>te(e)}}function te(e){E.value=e,N.value=!0}async function V(e){if(u.value){e&&($.value=!0);try{c.value=await mt(u.value),f.value=null,C.value=!0}catch(r){f.value=_e(r)}finally{$.value=!1}}}async function he(){if(u.value)try{const e=await yt(u.value);b.value=e.name}catch{}}async function me(e,r,k){const q=`${e.id}:${r}`;S.value={...S.value,[q]:!0};try{r==="start"?await gt(u.value,e.id):r==="stop"?await pt(u.value,e.id):await bt(u.value,e.id),o.success(`${k} requested for ${e.name}`),await V(!1)}catch(K){o.error(_e(K))}finally{const K={...S.value};delete K[q],S.value=K}}function ve(){P===null&&(P=setInterval(()=>{V(!1)},er))}function ge(){P!==null&&(clearInterval(P),P=null)}async function re(){C.value=!1,await Promise.all([he(),V(!0)])}return Pe(u,()=>{E.value=null,N.value=!1,re()}),st(()=>{re(),ve()}),it(()=>{ge()}),(e,r)=>(d(),T(w(ne),{vertical:"",size:16},{default:y(()=>[B("nav",jt,[x(w(lt),{to:"/servers"},{default:y(()=>[...r[6]||(r[6]=[D("Servers",-1)])]),_:1}),r[7]||(r[7]=B("span",{class:"breadcrumb__sep"},"/",-1)),B("span",Xt,G(b.value||u.value),1)]),x(w(dt),null,{header:y(()=>[x(w(ne),{align:"center",justify:"space-between"},{default:y(()=>[x(w(ne),{align:"center",size:10},{default:y(()=>[x(w(we),{strong:""},{default:y(()=>[...r[8]||(r[8]=[D("Containers",-1)])]),_:1}),b.value?(d(),T(w(we),{key:0,depth:"3"},{default:y(()=>[D(G(b.value),1)]),_:1})):pe("",!0)]),_:1}),x(w(ye),{secondary:"",loading:$.value,onClick:r[0]||(r[0]=k=>V(!0))},{default:y(()=>[...r[9]||(r[9]=[D(" Refresh ",-1)])]),_:1},8,["loading"])]),_:1})]),default:y(()=>[f.value?(d(),T(w(Ae),{key:0,type:"error","show-icon":!0,style:{"margin-bottom":"12px"}},{default:y(()=>[D(G(f.value),1)]),_:1})):pe("",!0),B("div",Yt,[B("div",Vt,[B("button",{class:be(["chip",{"is-active":v.value==="all"}]),type:"button","aria-pressed":v.value==="all",onClick:r[1]||(r[1]=k=>v.value="all")},[r[10]||(r[10]=D(" All ",-1)),B("span",Kt,G(L.value.all),1)],10,qt),B("button",{class:be(["chip",{"is-active":v.value==="running"}]),type:"button","aria-pressed":v.value==="running",onClick:r[2]||(r[2]=k=>v.value="running")},[r[11]||(r[11]=D(" Running ",-1)),B("span",Qt,G(L.value.running),1)],10,Gt),B("button",{class:be(["chip",{"is-active":v.value==="exited"}]),type:"button","aria-pressed":v.value==="exited",onClick:r[3]||(r[3]=k=>v.value="exited")},[r[12]||(r[12]=D(" Exited ",-1)),B("span",Zt,G(L.value.exited),1)],10,Jt)]),x(w(We),{value:F.value,"onUpdate:value":r[4]||(r[4]=k=>F.value=k),class:"search-input",placeholder:"Search container or image…","aria-label":"Search containers",clearable:""},{prefix:y(()=>[x(w(ct),null,{default:y(()=>[x(Ct,{name:"search"})]),_:1})]),_:1},8,["value"])]),x(w(ft),{columns:ue,data:X.value,loading:$.value,"row-key":ce,"row-props":fe,bordered:!1,"scroll-x":1200,pagination:{pageSize:10}},{empty:y(()=>[x(w(He),{description:C.value?c.value.length===0?"No containers on this node.":"No containers match the current filter.":"Loading containers…"},null,8,["description"])]),_:1},8,["data","loading"])]),_:1}),x(w(Dt),{show:N.value,"onUpdate:show":r[5]||(r[5]=k=>N.value=k),width:Z.value,placement:"right"},{default:y(()=>[x(w(Ut),{closable:"","native-scrollbar":!1},{default:y(()=>[E.value?(d(),T(vt,{key:0,"server-id":u.value,"container-id":E.value.id,title:E.value.name,subtitle:E.value.image,"auto-start-stream":!0},null,8,["server-id","container-id","title","subtitle"])):pe("",!0)]),_:1})]),_:1},8,["show","width"])]),_:1}))}}),_r=St(tr,[["__scopeId","data-v-322ada29"]]);export{_r as default};
