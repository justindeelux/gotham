import{d as V,O as U,P as N,o as u,c as C,p as S,bP as K,aA as ye,bJ as T,_ as q,bc as Z,bL as ze,az as _,bK as J,bQ as $e,j as $,aT as Se,bO as Q,bd as G,bR as xe,bS as ke,Z as Be,M as Ce,bT as Ee,f as g,bU as Re,b5 as F,bV as Me,bW as Fe,bX as Te,b9 as o,bY as H,ba as c,bZ as b,b_ as w,b$ as He,bb as ee,c0 as Ae,c1 as Ie,c2 as Oe,c3 as We,c4 as De,c5 as y,c6 as Pe,c7 as Le,c8 as j}from"./index-DWISmhN4.js";import{f as X}from"./format-length-B-p6aW7q.js";import{u as Y}from"./use-merged-state-BDUPS0y_.js";const Ue=["onMouseenter","onMouseleave","onMousedown"],_e={key:1,role:"none"};var Ne=V({name:"NDrawerContent",inheritAttrs:!1,props:{blockScroll:Boolean,show:{type:Boolean,default:void 0},displayDirective:{type:String,required:!0},placement:{type:String,required:!0},contentClass:String,contentStyle:[Object,String],nativeScrollbar:{type:Boolean,required:!0},scrollbarProps:Object,trapFocus:{type:Boolean,default:!0},autoFocus:{type:Boolean,default:!0},showMask:{type:[Boolean,String],required:!0},maxWidth:Number,maxHeight:Number,minWidth:Number,minHeight:Number,resizable:Boolean,onClickoutside:Function,onAfterLeave:Function,onAfterEnter:Function,onEsc:Function},setup(e){const n=$(!!e.show),t=$(null),v=Se(Q);let f=0,z="",h=null;const p=$(!1),m=$(!1),x=g(()=>e.placement==="top"||e.placement==="bottom"),{mergedClsPrefixRef:A,mergedRtlRef:I}=G(e),O=xe("Drawer",I,A),E=r,W=i=>{m.value=!0,f=x.value?i.clientY:i.clientX,z=document.body.style.cursor,document.body.style.cursor=x.value?"ns-resize":"ew-resize",document.body.addEventListener("mousemove",B),document.body.addEventListener("mouseleave",E),document.body.addEventListener("mouseup",r)},R=()=>{h!==null&&(window.clearTimeout(h),h=null),m.value?p.value=!0:h=window.setTimeout(()=>{p.value=!0},300)},D=()=>{h!==null&&(window.clearTimeout(h),h=null),p.value=!1},{doUpdateHeight:P,doUpdateWidth:L}=v,k=i=>{const{maxWidth:s}=e;if(s&&i>s)return s;const{minWidth:l}=e;return l&&i<l?l:i},M=i=>{const{maxHeight:s}=e;if(s&&i>s)return s;const{minHeight:l}=e;return l&&i<l?l:i};function B(i){if(m.value)if(x.value){let s=t.value?.offsetHeight||0;const l=f-i.clientY;s+=e.placement==="bottom"?l:-l,s=M(s),P(s),f=i.clientY}else{let s=t.value?.offsetWidth||0;const l=f-i.clientX;s+=e.placement==="right"?l:-l,s=k(s),L(s),f=i.clientX}}function r(){m.value&&(f=0,m.value=!1,document.body.style.cursor=z,document.body.removeEventListener("mousemove",B),document.body.removeEventListener("mouseup",r),document.body.removeEventListener("mouseleave",E))}ke(()=>{e.show&&(n.value=!0)}),Be(()=>e.show,i=>{i||r()}),Ce(()=>{r()});const a=g(()=>{const{show:i}=e,s=[[N,i]];return e.showMask||s.push([Re,e.onClickoutside,void 0,{capture:!0}]),s});function d(){n.value=!1,e.onAfterLeave?.()}return Ee(g(()=>e.blockScroll&&n.value)),F(Me,t),F(Fe,null),F(Te,null),{bodyRef:t,rtlEnabled:O,mergedClsPrefix:v.mergedClsPrefixRef,isMounted:v.isMountedRef,mergedTheme:v.mergedThemeRef,displayed:n,transitionName:g(()=>({right:"slide-in-from-right-transition",left:"slide-in-from-left-transition",top:"slide-in-from-top-transition",bottom:"slide-in-from-bottom-transition"})[e.placement]),handleAfterLeave:d,bodyDirectives:a,handleMousedownResizeTrigger:W,handleMouseenterResizeTrigger:R,handleMouseleaveResizeTrigger:D,isDragging:m,isHoverOnResizeTrigger:p}},render(){const{$slots:e,mergedClsPrefix:n}=this;return this.displayDirective==="show"||this.displayed||this.show?U((u(),C("div",_e,[(u(),S($e,{disabled:!this.showMask||!this.trapFocus,active:this.show,autoFocus:this.autoFocus,onEsc:this.onEsc},{default:()=>(u(),S(K,{name:this.transitionName,appear:this.isMounted,onAfterEnter:this.onAfterEnter,onAfterLeave:this.handleAfterLeave},{default:()=>U(ye("div",_(this.$attrs,{role:"dialog",ref:"bodyRef","aria-modal":"true",class:[`${n}-drawer`,this.rtlEnabled&&`${n}-drawer--rtl`,`${n}-drawer--${this.placement}-placement`,this.isDragging&&`${n}-drawer--unselectable`,this.nativeScrollbar&&`${n}-drawer--native-scrollbar`]}),[this.resizable?(u(),C("div",{key:2,class:T([`${n}-drawer__resize-trigger`,(this.isDragging||this.isHoverOnResizeTrigger)&&`${n}-drawer__resize-trigger--hover`]),onMouseenter:this.handleMouseenterResizeTrigger,onMouseleave:this.handleMouseleaveResizeTrigger,onMousedown:this.handleMousedownResizeTrigger},null,42,Ue)):null,this.nativeScrollbar?(u(),C("div",{key:3,class:T([`${n}-drawer-content-wrapper`,this.contentClass]),style:q(this.contentStyle),role:"none"},[Z(()=>e.default?.())],6)):(u(),S(ze,_({key:4},this.scrollbarProps,{contentStyle:this.contentStyle,contentClass:[`${n}-drawer-content-wrapper`,this.contentClass],theme:this.mergedTheme.peers.Scrollbar,themeOverrides:this.mergedTheme.peerOverrides.Scrollbar}),J(e),1040,["contentStyle","contentClass","theme","themeOverrides"]))]),this.bodyDirectives)},1032,["name","appear","onAfterEnter","onAfterLeave"]))},1032,["disabled","active","autoFocus","onEsc"]))])),[[N,this.displayDirective==="if"||this.displayed||this.show]]):null}});const{cubicBezierEaseIn:je,cubicBezierEaseOut:Xe}=H;function Ye({duration:e="0.3s",leaveDuration:n="0.2s",name:t="slide-in-from-bottom"}={}){return[o(`&.${t}-transition-leave-active`,{transition:`transform ${n} ${je}`}),o(`&.${t}-transition-enter-active`,{transition:`transform ${e} ${Xe}`}),o(`&.${t}-transition-enter-to`,{transform:"translateY(0)"}),o(`&.${t}-transition-enter-from`,{transform:"translateY(100%)"}),o(`&.${t}-transition-leave-from`,{transform:"translateY(0)"}),o(`&.${t}-transition-leave-to`,{transform:"translateY(100%)"})]}const{cubicBezierEaseIn:Ve,cubicBezierEaseOut:Ke}=H;function qe({duration:e="0.3s",leaveDuration:n="0.2s",name:t="slide-in-from-left"}={}){return[o(`&.${t}-transition-leave-active`,{transition:`transform ${n} ${Ve}`}),o(`&.${t}-transition-enter-active`,{transition:`transform ${e} ${Ke}`}),o(`&.${t}-transition-enter-to`,{transform:"translateX(0)"}),o(`&.${t}-transition-enter-from`,{transform:"translateX(-100%)"}),o(`&.${t}-transition-leave-from`,{transform:"translateX(0)"}),o(`&.${t}-transition-leave-to`,{transform:"translateX(-100%)"})]}const{cubicBezierEaseIn:Ze,cubicBezierEaseOut:Je}=H;function Qe({duration:e="0.3s",leaveDuration:n="0.2s",name:t="slide-in-from-right"}={}){return[o(`&.${t}-transition-leave-active`,{transition:`transform ${n} ${Ze}`}),o(`&.${t}-transition-enter-active`,{transition:`transform ${e} ${Je}`}),o(`&.${t}-transition-enter-to`,{transform:"translateX(0)"}),o(`&.${t}-transition-enter-from`,{transform:"translateX(100%)"}),o(`&.${t}-transition-leave-from`,{transform:"translateX(0)"}),o(`&.${t}-transition-leave-to`,{transform:"translateX(100%)"})]}const{cubicBezierEaseIn:Ge,cubicBezierEaseOut:et}=H;function tt({duration:e="0.3s",leaveDuration:n="0.2s",name:t="slide-in-from-top"}={}){return[o(`&.${t}-transition-leave-active`,{transition:`transform ${n} ${Ge}`}),o(`&.${t}-transition-enter-active`,{transition:`transform ${e} ${et}`}),o(`&.${t}-transition-enter-to`,{transform:"translateY(0)"}),o(`&.${t}-transition-enter-from`,{transform:"translateY(-100%)"}),o(`&.${t}-transition-leave-from`,{transform:"translateY(0)"}),o(`&.${t}-transition-leave-to`,{transform:"translateY(-100%)"})]}var rt=o([c("drawer",`
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
 `,[Qe(),qe(),tt(),Ye(),b("unselectable",`
 user-select: none; 
 -webkit-user-select: none;
 `),b("native-scrollbar",[c("drawer-content-wrapper",`
 overflow: auto;
 height: 100%;
 `)]),w("resize-trigger",`
 position: absolute;
 background-color: #0000;
 transition: background-color .3s var(--n-bezier);
 `,[b("hover",`
 background-color: var(--n-resize-trigger-color-hover);
 `)]),c("drawer-content-wrapper",`
 box-sizing: border-box;
 `),c("drawer-content",`
 height: 100%;
 display: flex;
 flex-direction: column;
 `,[b("native-scrollbar",[c("drawer-body-content-wrapper",`
 height: 100%;
 overflow: auto;
 `)]),c("drawer-body",`
 flex: 1 0 0;
 overflow: hidden;
 `),c("drawer-body-content-wrapper",`
 box-sizing: border-box;
 padding: var(--n-body-padding);
 `),c("drawer-header",`
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
 `,[w("main",`
 flex: 1;
 `),w("close",`
 margin-left: 6px;
 transition:
 background-color .3s var(--n-bezier),
 color .3s var(--n-bezier);
 `)]),c("drawer-footer",`
 display: flex;
 justify-content: flex-end;
 border-top: var(--n-footer-border-top);
 transition: border .3s var(--n-bezier);
 padding: var(--n-footer-padding);
 `)]),b("right-placement",`
 top: 0;
 bottom: 0;
 right: 0;
 border-top-left-radius: var(--n-border-radius);
 border-bottom-left-radius: var(--n-border-radius);
 `,[w("resize-trigger",`
 width: 3px;
 height: 100%;
 top: 0;
 left: 0;
 transform: translateX(-1.5px);
 cursor: ew-resize;
 `)]),b("left-placement",`
 top: 0;
 bottom: 0;
 left: 0;
 border-top-right-radius: var(--n-border-radius);
 border-bottom-right-radius: var(--n-border-radius);
 `,[w("resize-trigger",`
 width: 3px;
 height: 100%;
 top: 0;
 right: 0;
 transform: translateX(1.5px);
 cursor: ew-resize;
 `)]),b("top-placement",`
 top: 0;
 left: 0;
 right: 0;
 border-bottom-left-radius: var(--n-border-radius);
 border-bottom-right-radius: var(--n-border-radius);
 `,[w("resize-trigger",`
 width: 100%;
 height: 3px;
 bottom: 0;
 left: 0;
 transform: translateY(1.5px);
 cursor: ns-resize;
 `)]),b("bottom-placement",`
 left: 0;
 bottom: 0;
 right: 0;
 border-top-left-radius: var(--n-border-radius);
 border-top-right-radius: var(--n-border-radius);
 `,[w("resize-trigger",`
 width: 100%;
 height: 3px;
 top: 0;
 left: 0;
 transform: translateY(-1.5px);
 cursor: ns-resize;
 `)])]),o("body",[o(">",[c("drawer-container",`
 position: fixed;
 `)])]),c("drawer-container",`
 position: relative;
 position: absolute;
 left: 0;
 right: 0;
 top: 0;
 bottom: 0;
 pointer-events: none;
 `,[o("> *",`
 pointer-events: all;
 `)]),c("drawer-mask",`
 background-color: rgba(0, 0, 0, .3);
 position: absolute;
 left: 0;
 right: 0;
 top: 0;
 bottom: 0;
 `,[b("invisible",`
 background-color: rgba(0, 0, 0, 0)
 `),He({enterDuration:"0.2s",leaveDuration:"0.2s",enterCubicBezier:"var(--n-bezier-in)",leaveCubicBezier:"var(--n-bezier-out)"})])]);const ot=["onClick"],nt={...ee.props,show:Boolean,width:[Number,String],height:[Number,String],placement:{type:String,default:"right"},maskClosable:{type:Boolean,default:!0},showMask:{type:[Boolean,String],default:!0},to:[String,Object],displayDirective:{type:String,default:"if"},nativeScrollbar:{type:Boolean,default:!0},zIndex:Number,onMaskClick:Function,scrollbarProps:Object,contentClass:String,contentStyle:[Object,String],trapFocus:{type:Boolean,default:!0},onEsc:Function,autoFocus:{type:Boolean,default:!0},closeOnEsc:{type:Boolean,default:!0},blockScroll:{type:Boolean,default:!0},maxWidth:Number,maxHeight:Number,minWidth:Number,minHeight:Number,resizable:Boolean,defaultWidth:{type:[Number,String],default:251},defaultHeight:{type:[Number,String],default:251},onUpdateWidth:[Function,Array],onUpdateHeight:[Function,Array],"onUpdate:width":[Function,Array],"onUpdate:height":[Function,Array],"onUpdate:show":[Function,Array],onUpdateShow:[Function,Array],onAfterEnter:Function,onAfterLeave:Function,drawerStyle:[String,Object],drawerClass:String,target:null,onShow:Function,onHide:Function};var lt=V({name:"Drawer",inheritAttrs:!1,props:nt,setup(e){const{mergedClsPrefixRef:n,namespaceRef:t,inlineThemeDisabled:v}=G(e),f=Ie(),z=ee("Drawer","-drawer",rt,Le,e,n),h=$(e.defaultWidth),p=$(e.defaultHeight),m=Y(j(e,"width"),h),x=Y(j(e,"height"),p),A=g(()=>{const{placement:r}=e;return r==="top"||r==="bottom"?"":X(m.value)}),I=g(()=>{const{placement:r}=e;return r==="left"||r==="right"?"":X(x.value)}),O=r=>{const{onUpdateWidth:a,"onUpdate:width":d}=e;a&&y(a,r),d&&y(d,r),h.value=r},E=r=>{const{onUpdateHeight:a,"onUpdate:width":d}=e;a&&y(a,r),d&&y(d,r),p.value=r},W=g(()=>[{width:A.value,height:I.value},e.drawerStyle||""]);function R(r){const{onMaskClick:a,maskClosable:d}=e;d&&k(!1),a&&a(r)}function D(r){R(r)}const P=Oe();function L(r){e.onEsc?.(),e.show&&e.closeOnEsc&&De(r)&&(P.value||k(!1))}function k(r){const{onHide:a,onUpdateShow:d,"onUpdate:show":i}=e;d&&y(d,r),i&&y(i,r),a&&!r&&y(a,r)}F(Q,{isMountedRef:f,mergedThemeRef:z,mergedClsPrefixRef:n,doUpdateShow:k,doUpdateHeight:E,doUpdateWidth:O});const M=g(()=>{const{common:{cubicBezierEaseInOut:r,cubicBezierEaseIn:a,cubicBezierEaseOut:d},self:{color:i,textColor:s,boxShadow:l,lineHeight:te,headerPadding:re,footerPadding:oe,borderRadius:ne,bodyPadding:ie,titleFontSize:se,titleTextColor:ae,titleFontWeight:le,headerBorderBottom:de,footerBorderTop:ce,closeIconColor:ue,closeIconColorHover:he,closeIconColorPressed:fe,closeColorHover:be,closeColorPressed:me,closeIconSize:ge,closeSize:ve,closeBorderRadius:pe,resizableTriggerColorHover:we}}=z.value;return{"--n-line-height":te,"--n-color":i,"--n-border-radius":ne,"--n-text-color":s,"--n-box-shadow":l,"--n-bezier":r,"--n-bezier-out":d,"--n-bezier-in":a,"--n-header-padding":re,"--n-body-padding":ie,"--n-footer-padding":oe,"--n-title-text-color":ae,"--n-title-font-size":se,"--n-title-font-weight":le,"--n-header-border-bottom":de,"--n-footer-border-top":ce,"--n-close-icon-color":ue,"--n-close-icon-color-hover":he,"--n-close-icon-color-pressed":fe,"--n-close-size":ve,"--n-close-color-hover":be,"--n-close-color-pressed":me,"--n-close-icon-size":ge,"--n-close-border-radius":pe,"--n-resize-trigger-color-hover":we}}),B=v?We("drawer",void 0,M,e):void 0;return{mergedClsPrefix:n,namespace:t,mergedBodyStyle:W,handleOutsideClick:D,handleMaskClick:R,handleEsc:L,mergedTheme:z,cssVars:v?void 0:M,themeClass:B?.themeClass,onRender:B?.onRender,isMounted:f}},render(){const{mergedClsPrefix:e}=this;return u(),S(Ae,{to:this.to,show:this.show},{default:()=>(this.onRender?.(),U((u(),C("div",{class:T([`${e}-drawer-container`,this.namespace,this.themeClass]),style:q(this.cssVars),role:"none"},[this.showMask?(u(),S(K,{key:0,name:"fade-in-transition",appear:this.isMounted},{default:()=>this.show?(u(),C("div",{key:1,"aria-hidden":!0,class:T([`${e}-drawer-mask`,this.showMask==="transparent"&&`${e}-drawer-mask--invisible`]),onClick:this.handleMaskClick},null,10,ot)):null},1032,["appear"])):Z(()=>null),(u(),S(Ne,_(this.$attrs,{class:[this.drawerClass,this.$attrs.class],style:[this.mergedBodyStyle,this.$attrs.style],blockScroll:this.blockScroll,contentStyle:this.contentStyle,contentClass:this.contentClass,placement:this.placement,scrollbarProps:this.scrollbarProps,show:this.show,displayDirective:this.displayDirective,nativeScrollbar:this.nativeScrollbar,onAfterEnter:this.onAfterEnter,onAfterLeave:this.onAfterLeave,trapFocus:this.trapFocus,autoFocus:this.autoFocus,resizable:this.resizable,maxHeight:this.maxHeight,minHeight:this.minHeight,maxWidth:this.maxWidth,minWidth:this.minWidth,showMask:this.showMask,onEsc:this.handleEsc,onClickoutside:this.handleOutsideClick}),J(this.$slots),1040,["class","style","blockScroll","contentStyle","contentClass","placement","scrollbarProps","show","displayDirective","nativeScrollbar","onAfterEnter","onAfterLeave","trapFocus","autoFocus","resizable","maxHeight","minHeight","maxWidth","minWidth","showMask","onEsc","onClickoutside"]))],6)),[[Pe,{zIndex:this.zIndex,enabled:this.show}]]))},1032,["to","show"])}});export{lt as D};
