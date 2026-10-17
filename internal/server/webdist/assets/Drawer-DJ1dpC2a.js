import{d as V,aU as L,aV as N,o as u,c as C,q as S,aW as q,$ as ye,aO as H,aG as K,ab as Q,aQ as ze,aa as _,aP as G,aX as $e,k as $,a1 as Se,aT as Z,ac as J,aY as ke,aZ as xe,Q as Be,a0 as Ce,a_ as Ee,g,a$ as Re,a3 as M,b0 as Fe,b1 as Me,b2 as He,a7 as o,b3 as T,a8 as c,b4 as m,b5 as w,b6 as Te,a9 as ee,b7 as Ie,b8 as We,b9 as Ae,ba as De,bb as Oe,bc as y,bd as Pe,be as Ue,bf as X}from"./index-CeqxtdRb.js";import{f as Y}from"./format-length-B-p6aW7q.js";import{u as j}from"./use-merged-state-Bd9BsELH.js";const Le=["onMouseenter","onMouseleave","onMousedown"],_e={key:1,role:"none"};var Ne=V({name:"NDrawerContent",inheritAttrs:!1,props:{blockScroll:Boolean,show:{type:Boolean,default:void 0},displayDirective:{type:String,required:!0},placement:{type:String,required:!0},contentClass:String,contentStyle:[Object,String],nativeScrollbar:{type:Boolean,required:!0},scrollbarProps:Object,trapFocus:{type:Boolean,default:!0},autoFocus:{type:Boolean,default:!0},showMask:{type:[Boolean,String],required:!0},maxWidth:Number,maxHeight:Number,minWidth:Number,minHeight:Number,resizable:Boolean,onClickoutside:Function,onAfterLeave:Function,onAfterEnter:Function,onEsc:Function},setup(e){const n=$(!!e.show),t=$(null),v=Se(Z);let f=0,z="",h=null;const p=$(!1),b=$(!1),k=g(()=>e.placement==="top"||e.placement==="bottom"),{mergedClsPrefixRef:I,mergedRtlRef:W}=J(e),A=ke("Drawer",W,I),E=r,D=i=>{b.value=!0,f=k.value?i.clientY:i.clientX,z=document.body.style.cursor,document.body.style.cursor=k.value?"ns-resize":"ew-resize",document.body.addEventListener("mousemove",B),document.body.addEventListener("mouseleave",E),document.body.addEventListener("mouseup",r)},R=()=>{h!==null&&(window.clearTimeout(h),h=null),b.value?p.value=!0:h=window.setTimeout(()=>{p.value=!0},300)},O=()=>{h!==null&&(window.clearTimeout(h),h=null),p.value=!1},{doUpdateHeight:P,doUpdateWidth:U}=v,x=i=>{const{maxWidth:a}=e;if(a&&i>a)return a;const{minWidth:l}=e;return l&&i<l?l:i},F=i=>{const{maxHeight:a}=e;if(a&&i>a)return a;const{minHeight:l}=e;return l&&i<l?l:i};function B(i){if(b.value)if(k.value){let a=t.value?.offsetHeight||0;const l=f-i.clientY;a+=e.placement==="bottom"?l:-l,a=F(a),P(a),f=i.clientY}else{let a=t.value?.offsetWidth||0;const l=f-i.clientX;a+=e.placement==="right"?l:-l,a=x(a),U(a),f=i.clientX}}function r(){b.value&&(f=0,b.value=!1,document.body.style.cursor=z,document.body.removeEventListener("mousemove",B),document.body.removeEventListener("mouseup",r),document.body.removeEventListener("mouseleave",E))}xe(()=>{e.show&&(n.value=!0)}),Be(()=>e.show,i=>{i||r()}),Ce(()=>{r()});const s=g(()=>{const{show:i}=e,a=[[N,i]];return e.showMask||a.push([Re,e.onClickoutside,void 0,{capture:!0}]),a});function d(){n.value=!1,e.onAfterLeave?.()}return Ee(g(()=>e.blockScroll&&n.value)),M(Fe,t),M(Me,null),M(He,null),{bodyRef:t,rtlEnabled:A,mergedClsPrefix:v.mergedClsPrefixRef,isMounted:v.isMountedRef,mergedTheme:v.mergedThemeRef,displayed:n,transitionName:g(()=>({right:"slide-in-from-right-transition",left:"slide-in-from-left-transition",top:"slide-in-from-top-transition",bottom:"slide-in-from-bottom-transition"})[e.placement]),handleAfterLeave:d,bodyDirectives:s,handleMousedownResizeTrigger:D,handleMouseenterResizeTrigger:R,handleMouseleaveResizeTrigger:O,isDragging:b,isHoverOnResizeTrigger:p}},render(){const{$slots:e,mergedClsPrefix:n}=this;return this.displayDirective==="show"||this.displayed||this.show?L((u(),C("div",_e,[(u(),S($e,{disabled:!this.showMask||!this.trapFocus,active:this.show,autoFocus:this.autoFocus,onEsc:this.onEsc},{default:()=>(u(),S(q,{name:this.transitionName,appear:this.isMounted,onAfterEnter:this.onAfterEnter,onAfterLeave:this.handleAfterLeave},{default:()=>L(ye("div",_(this.$attrs,{role:"dialog",ref:"bodyRef","aria-modal":"true",class:[`${n}-drawer`,this.rtlEnabled&&`${n}-drawer--rtl`,`${n}-drawer--${this.placement}-placement`,this.isDragging&&`${n}-drawer--unselectable`,this.nativeScrollbar&&`${n}-drawer--native-scrollbar`]}),[this.resizable?(u(),C("div",{key:2,class:H([`${n}-drawer__resize-trigger`,(this.isDragging||this.isHoverOnResizeTrigger)&&`${n}-drawer__resize-trigger--hover`]),onMouseenter:this.handleMouseenterResizeTrigger,onMouseleave:this.handleMouseleaveResizeTrigger,onMousedown:this.handleMousedownResizeTrigger},null,42,Le)):null,this.nativeScrollbar?(u(),C("div",{key:3,class:H([`${n}-drawer-content-wrapper`,this.contentClass]),style:K(this.contentStyle),role:"none"},[Q(()=>e.default?.())],6)):(u(),S(ze,_({key:4},this.scrollbarProps,{contentStyle:this.contentStyle,contentClass:[`${n}-drawer-content-wrapper`,this.contentClass],theme:this.mergedTheme.peers.Scrollbar,themeOverrides:this.mergedTheme.peerOverrides.Scrollbar}),G(e),1040,["contentStyle","contentClass","theme","themeOverrides"]))]),this.bodyDirectives)},1032,["name","appear","onAfterEnter","onAfterLeave"]))},1032,["disabled","active","autoFocus","onEsc"]))])),[[N,this.displayDirective==="if"||this.displayed||this.show]]):null}});const{cubicBezierEaseIn:Xe,cubicBezierEaseOut:Ye}=T;function je({duration:e="0.3s",leaveDuration:n="0.2s",name:t="slide-in-from-bottom"}={}){return[o(`&.${t}-transition-leave-active`,{transition:`transform ${n} ${Xe}`}),o(`&.${t}-transition-enter-active`,{transition:`transform ${e} ${Ye}`}),o(`&.${t}-transition-enter-to`,{transform:"translateY(0)"}),o(`&.${t}-transition-enter-from`,{transform:"translateY(100%)"}),o(`&.${t}-transition-leave-from`,{transform:"translateY(0)"}),o(`&.${t}-transition-leave-to`,{transform:"translateY(100%)"})]}const{cubicBezierEaseIn:Ve,cubicBezierEaseOut:qe}=T;function Ke({duration:e="0.3s",leaveDuration:n="0.2s",name:t="slide-in-from-left"}={}){return[o(`&.${t}-transition-leave-active`,{transition:`transform ${n} ${Ve}`}),o(`&.${t}-transition-enter-active`,{transition:`transform ${e} ${qe}`}),o(`&.${t}-transition-enter-to`,{transform:"translateX(0)"}),o(`&.${t}-transition-enter-from`,{transform:"translateX(-100%)"}),o(`&.${t}-transition-leave-from`,{transform:"translateX(0)"}),o(`&.${t}-transition-leave-to`,{transform:"translateX(-100%)"})]}const{cubicBezierEaseIn:Qe,cubicBezierEaseOut:Ge}=T;function Ze({duration:e="0.3s",leaveDuration:n="0.2s",name:t="slide-in-from-right"}={}){return[o(`&.${t}-transition-leave-active`,{transition:`transform ${n} ${Qe}`}),o(`&.${t}-transition-enter-active`,{transition:`transform ${e} ${Ge}`}),o(`&.${t}-transition-enter-to`,{transform:"translateX(0)"}),o(`&.${t}-transition-enter-from`,{transform:"translateX(100%)"}),o(`&.${t}-transition-leave-from`,{transform:"translateX(0)"}),o(`&.${t}-transition-leave-to`,{transform:"translateX(100%)"})]}const{cubicBezierEaseIn:Je,cubicBezierEaseOut:et}=T;function tt({duration:e="0.3s",leaveDuration:n="0.2s",name:t="slide-in-from-top"}={}){return[o(`&.${t}-transition-leave-active`,{transition:`transform ${n} ${Je}`}),o(`&.${t}-transition-enter-active`,{transition:`transform ${e} ${et}`}),o(`&.${t}-transition-enter-to`,{transform:"translateY(0)"}),o(`&.${t}-transition-enter-from`,{transform:"translateY(-100%)"}),o(`&.${t}-transition-leave-from`,{transform:"translateY(0)"}),o(`&.${t}-transition-leave-to`,{transform:"translateY(-100%)"})]}var rt=o([c("drawer",`
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
 `,[Ze(),Ke(),tt(),je(),m("unselectable",`
 user-select: none; 
 -webkit-user-select: none;
 `),m("native-scrollbar",[c("drawer-content-wrapper",`
 overflow: auto;
 height: 100%;
 `)]),w("resize-trigger",`
 position: absolute;
 background-color: #0000;
 transition: background-color .3s var(--n-bezier);
 `,[m("hover",`
 background-color: var(--n-resize-trigger-color-hover);
 `)]),c("drawer-content-wrapper",`
 box-sizing: border-box;
 `),c("drawer-content",`
 height: 100%;
 display: flex;
 flex-direction: column;
 `,[m("native-scrollbar",[c("drawer-body-content-wrapper",`
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
 `)]),m("right-placement",`
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
 `)]),m("left-placement",`
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
 `)]),m("top-placement",`
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
 `)]),m("bottom-placement",`
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
 `,[m("invisible",`
 background-color: rgba(0, 0, 0, 0)
 `),Te({enterDuration:"0.2s",leaveDuration:"0.2s",enterCubicBezier:"var(--n-bezier-in)",leaveCubicBezier:"var(--n-bezier-out)"})])]);const ot=["onClick"],nt={...ee.props,show:Boolean,width:[Number,String],height:[Number,String],placement:{type:String,default:"right"},maskClosable:{type:Boolean,default:!0},showMask:{type:[Boolean,String],default:!0},to:[String,Object],displayDirective:{type:String,default:"if"},nativeScrollbar:{type:Boolean,default:!0},zIndex:Number,onMaskClick:Function,scrollbarProps:Object,contentClass:String,contentStyle:[Object,String],trapFocus:{type:Boolean,default:!0},onEsc:Function,autoFocus:{type:Boolean,default:!0},closeOnEsc:{type:Boolean,default:!0},blockScroll:{type:Boolean,default:!0},maxWidth:Number,maxHeight:Number,minWidth:Number,minHeight:Number,resizable:Boolean,defaultWidth:{type:[Number,String],default:251},defaultHeight:{type:[Number,String],default:251},onUpdateWidth:[Function,Array],onUpdateHeight:[Function,Array],"onUpdate:width":[Function,Array],"onUpdate:height":[Function,Array],"onUpdate:show":[Function,Array],onUpdateShow:[Function,Array],onAfterEnter:Function,onAfterLeave:Function,drawerStyle:[String,Object],drawerClass:String,target:null,onShow:Function,onHide:Function};var lt=V({name:"Drawer",inheritAttrs:!1,props:nt,setup(e){const{mergedClsPrefixRef:n,namespaceRef:t,inlineThemeDisabled:v}=J(e),f=We(),z=ee("Drawer","-drawer",rt,Ue,e,n),h=$(e.defaultWidth),p=$(e.defaultHeight),b=j(X(e,"width"),h),k=j(X(e,"height"),p),I=g(()=>{const{placement:r}=e;return r==="top"||r==="bottom"?"":Y(b.value)}),W=g(()=>{const{placement:r}=e;return r==="left"||r==="right"?"":Y(k.value)}),A=r=>{const{onUpdateWidth:s,"onUpdate:width":d}=e;s&&y(s,r),d&&y(d,r),h.value=r},E=r=>{const{onUpdateHeight:s,"onUpdate:width":d}=e;s&&y(s,r),d&&y(d,r),p.value=r},D=g(()=>[{width:I.value,height:W.value},e.drawerStyle||""]);function R(r){const{onMaskClick:s,maskClosable:d}=e;d&&x(!1),s&&s(r)}function O(r){R(r)}const P=Ae();function U(r){e.onEsc?.(),e.show&&e.closeOnEsc&&Oe(r)&&(P.value||x(!1))}function x(r){const{onHide:s,onUpdateShow:d,"onUpdate:show":i}=e;d&&y(d,r),i&&y(i,r),s&&!r&&y(s,r)}M(Z,{isMountedRef:f,mergedThemeRef:z,mergedClsPrefixRef:n,doUpdateShow:x,doUpdateHeight:E,doUpdateWidth:A});const F=g(()=>{const{common:{cubicBezierEaseInOut:r,cubicBezierEaseIn:s,cubicBezierEaseOut:d},self:{color:i,textColor:a,boxShadow:l,lineHeight:te,headerPadding:re,footerPadding:oe,borderRadius:ne,bodyPadding:ie,titleFontSize:ae,titleTextColor:se,titleFontWeight:le,headerBorderBottom:de,footerBorderTop:ce,closeIconColor:ue,closeIconColorHover:he,closeIconColorPressed:fe,closeColorHover:me,closeColorPressed:be,closeIconSize:ge,closeSize:ve,closeBorderRadius:pe,resizableTriggerColorHover:we}}=z.value;return{"--n-line-height":te,"--n-color":i,"--n-border-radius":ne,"--n-text-color":a,"--n-box-shadow":l,"--n-bezier":r,"--n-bezier-out":d,"--n-bezier-in":s,"--n-header-padding":re,"--n-body-padding":ie,"--n-footer-padding":oe,"--n-title-text-color":se,"--n-title-font-size":ae,"--n-title-font-weight":le,"--n-header-border-bottom":de,"--n-footer-border-top":ce,"--n-close-icon-color":ue,"--n-close-icon-color-hover":he,"--n-close-icon-color-pressed":fe,"--n-close-size":ve,"--n-close-color-hover":me,"--n-close-color-pressed":be,"--n-close-icon-size":ge,"--n-close-border-radius":pe,"--n-resize-trigger-color-hover":we}}),B=v?De("drawer",void 0,F,e):void 0;return{mergedClsPrefix:n,namespace:t,mergedBodyStyle:D,handleOutsideClick:O,handleMaskClick:R,handleEsc:U,mergedTheme:z,cssVars:v?void 0:F,themeClass:B?.themeClass,onRender:B?.onRender,isMounted:f}},render(){const{mergedClsPrefix:e}=this;return u(),S(Ie,{to:this.to,show:this.show},{default:()=>(this.onRender?.(),L((u(),C("div",{class:H([`${e}-drawer-container`,this.namespace,this.themeClass]),style:K(this.cssVars),role:"none"},[this.showMask?(u(),S(q,{key:0,name:"fade-in-transition",appear:this.isMounted},{default:()=>this.show?(u(),C("div",{key:1,"aria-hidden":!0,class:H([`${e}-drawer-mask`,this.showMask==="transparent"&&`${e}-drawer-mask--invisible`]),onClick:this.handleMaskClick},null,10,ot)):null},1032,["appear"])):Q(()=>null),(u(),S(Ne,_(this.$attrs,{class:[this.drawerClass,this.$attrs.class],style:[this.mergedBodyStyle,this.$attrs.style],blockScroll:this.blockScroll,contentStyle:this.contentStyle,contentClass:this.contentClass,placement:this.placement,scrollbarProps:this.scrollbarProps,show:this.show,displayDirective:this.displayDirective,nativeScrollbar:this.nativeScrollbar,onAfterEnter:this.onAfterEnter,onAfterLeave:this.onAfterLeave,trapFocus:this.trapFocus,autoFocus:this.autoFocus,resizable:this.resizable,maxHeight:this.maxHeight,minHeight:this.minHeight,maxWidth:this.maxWidth,minWidth:this.minWidth,showMask:this.showMask,onEsc:this.handleEsc,onClickoutside:this.handleOutsideClick}),G(this.$slots),1040,["class","style","blockScroll","contentStyle","contentClass","placement","scrollbarProps","show","displayDirective","nativeScrollbar","onAfterEnter","onAfterLeave","trapFocus","autoFocus","resizable","maxHeight","minHeight","maxWidth","minWidth","showMask","onEsc","onClickoutside"]))],6)),[[Pe,{zIndex:this.zIndex,enabled:this.show}]]))},1032,["to","show"])}});export{lt as D};
