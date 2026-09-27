import{N as mo,O as ne,q as h,P as fo,p as z,Q as go,S as v,d as j,y as Z,U as po,V as bo,g as u,c as I,z as R,E as Y,A as N,F as ce,W as X,G as de,H as f,r as M,o as xo,I as we,X as yo,Y as Co,h as E,Z as De,_ as se,$ as zo,M as So,a0 as wo,a1 as Io,a2 as Ro,a3 as Po,a4 as Oe,v as T,a5 as Ue,a6 as J,a7 as Ge,a8 as We,a9 as re,D as ee,aa as qe,ab as Q,ac as q,ad as ge,ae,af as ko,ag as _e,ah as te,ai as Te,aj as Ie,ak as _o,al as pe,am as To,an as Ao,ao as No,u as Ho,w as V,b as B,a as W,i as me,t as be,B as Fe,R as Eo,ap as Lo,f as Bo,e as Oo}from"./index-ByhPpcf2.js";import{t as Fo,C as $o,T as Mo,D as Ye,V as Ko,c as xe,_ as Vo}from"./_plugin-vue_export-helper-CMzUdJnJ.js";import{u as jo,t as $e,S as Me}from"./text-ud8cZo7M.js";import{f as ye,u as Re}from"./format-length-brmn2F-7.js";const Do=mo&&"loading"in document.createElement("img");function Uo(e={}){const{root:o=null}=e;return{hash:`${e.rootMargin||"0px 0px 0px 0px"}-${Array.isArray(e.threshold)?e.threshold.join(","):e.threshold??"0"}`,options:{...e,root:(typeof o=="string"?document.querySelector(o):o)||document.documentElement}}}const Ce=new WeakMap,ze=new WeakMap,Se=new WeakMap,Go=(e,o,r)=>{if(!e)return()=>{};const i=Uo(o),{root:a}=i.options;let l;const d=Ce.get(a);d?l=d:(l=new Map,Ce.set(a,l));let c,s;l.has(i.hash)?(s=l.get(i.hash),s[1].has(e)||(c=s[0],s[1].add(e),c.observe(e))):(c=new IntersectionObserver(C=>{C.forEach(b=>{if(b.isIntersecting){const P=ze.get(b.target),H=Se.get(b.target);P&&P(),H&&(H.value=!0)}})},i.options),c.observe(e),s=[c,new Set([e])],l.set(i.hash,s));let m=!1;const S=()=>{m||(ze.delete(e),Se.delete(e),m=!0,s[1].has(e)&&(s[0].unobserve(e),s[1].delete(e)),s[1].size<=0&&l.delete(i.hash),l.size||Ce.delete(a))};return ze.set(e,S),Se.set(e,r),S},Wo=ne("n-avatar-group");var qo=h("avatar",`
 width: var(--n-merged-size);
 height: var(--n-merged-size);
 color: #FFF;
 font-size: var(--n-font-size);
 display: inline-flex;
 position: relative;
 overflow: hidden;
 text-align: center;
 border: var(--n-border);
 border-radius: var(--n-border-radius);
 --n-merged-color: var(--n-color);
 background-color: var(--n-merged-color);
 transition:
 border-color .3s var(--n-bezier),
 background-color .3s var(--n-bezier),
 color .3s var(--n-bezier);
`,[fo(z("&","--n-merged-color: var(--n-color-modal);")),go(z("&","--n-merged-color: var(--n-color-popover);")),z("img",`
 width: 100%;
 height: 100%;
 `),v("text",`
 white-space: nowrap;
 display: inline-block;
 position: absolute;
 left: 50%;
 top: 50%;
 `),h("icon",`
 vertical-align: bottom;
 font-size: calc(var(--n-merged-size) - 6px);
 `),v("text","line-height: 1.25")]);const Yo=["src"],Xo={...Z.props,size:[String,Number],src:String,circle:{type:Boolean,default:void 0},objectFit:String,round:{type:Boolean,default:void 0},bordered:{type:Boolean,default:void 0},onError:Function,fallbackSrc:String,intersectionObserverOptions:Object,lazy:Boolean,onLoad:Function,renderPlaceholder:Function,renderFallback:Function,imgProps:Object,color:String};var Zo=j({name:"Avatar",props:Xo,slots:Object,setup(e){const{mergedClsPrefixRef:o,inlineThemeDisabled:r}=ce(e),i=M(!1);let a=null;const l=M(null),d=M(null),c=()=>{const{value:p}=l;if(p&&(a===null||a!==p.innerHTML)){a=p.innerHTML;const{value:k}=d;if(k){const{offsetWidth:$,offsetHeight:w}=k,{offsetWidth:x,offsetHeight:O}=p,D=.9,U=Math.min($/x*D,w/O*D,1);p.style.transform=`translateX(-50%) translateY(-50%) scale(${U})`}}},s=X(Wo,null),m=f(()=>{const{size:p}=e;if(p)return p;const{size:k}=s||{};return k||"medium"}),S=Z("Avatar","-avatar",qo,zo,e,o),C=X(Fo,null),b=f(()=>{if(s)return!0;const{round:p,circle:k}=e;return p!==void 0||k!==void 0?p||k:C?C.roundRef.value:!1}),P=f(()=>s?!0:e.bordered||!1),H=f(()=>{const p=m.value,k=b.value,$=P.value,{color:w}=e,{self:{borderRadius:x,fontSize:O,color:D,border:U,colorModal:le,colorPopover:K},common:{cubicBezierEaseInOut:ve}}=S.value;let ie;return typeof p=="number"?ie=`${p}px`:ie=S.value.self[So("height",p)],{"--n-font-size":O,"--n-border":$?U:"none","--n-border-radius":k?"50%":x,"--n-color":w||D,"--n-color-modal":w||le,"--n-color-popover":w||K,"--n-bezier":ve,"--n-merged-size":`var(--n-avatar-size-override, ${ie})`}}),A=r?de("avatar",f(()=>{const p=m.value,k=b.value,$=P.value,{color:w}=e;let x="";return p&&(typeof p=="number"?x+=`a${p}`:x+=p[0]),k&&(x+="b"),$&&(x+="c"),w&&(x+=wo(w)),x}),H,e):void 0,L=M(!e.lazy);xo(()=>{if(e.lazy&&e.intersectionObserverOptions){let p;const k=we(()=>{p?.(),p=void 0,e.lazy&&(p=Go(d.value,e.intersectionObserverOptions,L))});yo(()=>{k(),p?.()})}}),Co(()=>e.src||e.imgProps?.src,()=>{i.value=!1});const F=M(!e.lazy);return{textRef:l,selfRef:d,mergedRoundRef:b,mergedClsPrefix:o,fitTextTransform:c,cssVars:r?void 0:H,themeClass:A?.themeClass,onRender:A?.onRender,hasLoadError:i,shouldStartLoading:L,loaded:F,mergedOnError:p=>{if(!L.value)return;i.value=!0;const{onError:k,imgProps:{onError:$}={}}=e;k?.(p),$?.(p)},mergedOnLoad:p=>{const{onLoad:k,imgProps:{onLoad:$}={}}=e;k?.(p),$?.(p),F.value=!0}}},render(){const{$slots:e,src:o,mergedClsPrefix:r,lazy:i,onRender:a,loaded:l,hasLoadError:d,imgProps:c={}}=this;a?.();let s;const m=!l&&!d&&(this.renderPlaceholder?this.renderPlaceholder():this.$slots.placeholder?.());return this.hasLoadError?s=this.renderFallback?this.renderFallback():po(e.fallback,()=>[(u(),I("img",{src:this.fallbackSrc,style:Y({objectFit:this.objectFit})},null,12,Yo))]):s=bo(e.default,S=>{if(S)return u(),E(De,{key:1,onResize:this.fitTextTransform},{default:()=>(u(),I("span",{ref:"textRef",class:N(`${r}-avatar__text`)},[R(()=>S)],2))},1032,["onResize"]);if(o||c.src){const C=this.src||c.src;return se("img",{...c,loading:Do&&!this.intersectionObserverOptions&&i?"lazy":"eager",src:i&&this.intersectionObserverOptions?this.shouldStartLoading?C:void 0:C,"data-image-src":C,onLoad:this.mergedOnLoad,onError:this.mergedOnError,style:[c.style||"",{objectFit:this.objectFit},m?{height:"0",width:"0",visibility:"hidden",position:"absolute"}:""]})}}),u(),I("span",{ref:"selfRef",class:N([`${r}-avatar`,this.themeClass]),style:Y(this.cssVars)},[R(()=>s),R(()=>i&&m)],6)}});function Qo(e){const{baseColor:o,textColor2:r,bodyColor:i,cardColor:a,dividerColor:l,actionColor:d,scrollbarColor:c,scrollbarColorHover:s,invertedColor:m}=e;return{textColor:r,textColorInverted:"#FFF",color:i,colorEmbedded:d,headerColor:a,headerColorInverted:m,footerColor:d,footerColorInverted:m,headerBorderColor:l,headerBorderColorInverted:m,footerBorderColor:l,footerBorderColorInverted:m,siderBorderColor:l,siderBorderColorInverted:m,siderColor:a,siderColorInverted:m,siderToggleButtonBorder:`1px solid ${l}`,siderToggleButtonColor:o,siderToggleButtonIconColor:r,siderToggleButtonIconColorInverted:r,siderToggleBarColor:Oe(i,c),siderToggleBarColorHover:Oe(i,s),__invertScrollbar:"true"}}const Ae=Io({name:"Layout",common:Po,peers:{Scrollbar:Ro},self:Qo}),Xe=ne("n-layout-sider"),Ne={type:String,default:"static"};var Jo=h("layout",`
 color: var(--n-text-color);
 background-color: var(--n-color);
 box-sizing: border-box;
 position: relative;
 z-index: auto;
 flex: auto;
 overflow: hidden;
 transition:
 box-shadow .3s var(--n-bezier),
 background-color .3s var(--n-bezier),
 color .3s var(--n-bezier);
`,[h("layout-scroll-container",`
 overflow-x: hidden;
 box-sizing: border-box;
 height: 100%;
 `),T("absolute-positioned",`
 position: absolute;
 left: 0;
 right: 0;
 top: 0;
 bottom: 0;
 `)]);const et={embedded:Boolean,position:Ne,nativeScrollbar:{type:Boolean,default:!0},scrollbarProps:Object,onScroll:Function,contentClass:String,contentStyle:{type:[String,Object],default:""},hasSider:Boolean,siderPlacement:{type:String,default:"left"}},Ze=ne("n-layout");function Qe(e){return j({name:e?"LayoutContent":"Layout",props:{...Z.props,...et},setup(o){const r=M(null),i=M(null),{mergedClsPrefixRef:a,inlineThemeDisabled:l}=ce(o),d=Z("Layout","-layout",Jo,Ae,o,a);function c(A,L){if(o.nativeScrollbar){const{value:F}=r;F&&(L===void 0?F.scrollTo(A):F.scrollTo(A,L))}else{const{value:F}=i;F&&F.scrollTo(A,L)}}re(Ze,o);let s=0,m=0;const S=A=>{const L=A.target;s=L.scrollLeft,m=L.scrollTop,o.onScroll?.(A)};We(()=>{if(o.nativeScrollbar){const A=r.value;A&&(A.scrollTop=m,A.scrollLeft=s)}});const C={display:"flex",flexWrap:"nowrap",width:"100%",flexDirection:"row"},b={scrollTo:c},P=f(()=>{const{common:{cubicBezierEaseInOut:A},self:L}=d.value;return{"--n-bezier":A,"--n-color":o.embedded?L.colorEmbedded:L.color,"--n-text-color":L.textColor}}),H=l?de("layout",f(()=>o.embedded?"e":""),P,o):void 0;return{mergedClsPrefix:a,scrollableElRef:r,scrollbarInstRef:i,hasSiderStyle:C,mergedTheme:d,handleNativeElScroll:S,cssVars:l?void 0:P,themeClass:H?.themeClass,onRender:H?.onRender,...b}},render(){const{mergedClsPrefix:o,hasSider:r}=this;this.onRender?.();const i=r?this.hasSiderStyle:void 0,a=[this.themeClass,e&&`${o}-layout-content`,`${o}-layout`,`${o}-layout--${this.position}-positioned`];return u(),I("div",{class:N(a),style:Y(this.cssVars)},[this.nativeScrollbar?(u(),I("div",{key:0,ref:"scrollableElRef",class:N([`${o}-layout-scroll-container`,this.contentClass]),style:Y([this.contentStyle,i]),onScroll:this.handleNativeElScroll},[R(()=>this.$slots.default?.())],46,["onScroll"])):(u(),E(Ge,J({key:1},this.scrollbarProps,{onScroll:this.onScroll,ref:"scrollbarInstRef",theme:this.mergedTheme.peers.Scrollbar,themeOverrides:this.mergedTheme.peerOverrides.Scrollbar,contentClass:this.contentClass,contentStyle:[this.contentStyle,i]}),Ue(this.$slots),1040,["onScroll","theme","themeOverrides","contentClass","contentStyle"]))],6)}})}var Ke=Qe(!1),ot=Qe(!0),tt=h("layout-header",`
 transition:
 color .3s var(--n-bezier),
 background-color .3s var(--n-bezier),
 box-shadow .3s var(--n-bezier),
 border-color .3s var(--n-bezier);
 box-sizing: border-box;
 width: 100%;
 background-color: var(--n-color);
 color: var(--n-text-color);
`,[T("absolute-positioned",`
 position: absolute;
 left: 0;
 right: 0;
 top: 0;
 `),T("bordered",`
 border-bottom: solid 1px var(--n-border-color);
 `)]);const rt={position:Ne,inverted:Boolean,bordered:Boolean};var nt=j({name:"LayoutHeader",props:{...Z.props,...rt},setup(e){const{mergedClsPrefixRef:o,inlineThemeDisabled:r}=ce(e),i=Z("Layout","-layout-header",tt,Ae,e,o),a=f(()=>{const{common:{cubicBezierEaseInOut:d},self:c}=i.value,s={"--n-bezier":d};return e.inverted?(s["--n-color"]=c.headerColorInverted,s["--n-text-color"]=c.textColorInverted,s["--n-border-color"]=c.headerBorderColorInverted):(s["--n-color"]=c.headerColor,s["--n-text-color"]=c.textColor,s["--n-border-color"]=c.headerBorderColor),s}),l=r?de("layout-header",f(()=>e.inverted?"a":"b"),a,e):void 0;return{mergedClsPrefix:o,cssVars:r?void 0:a,themeClass:l?.themeClass,onRender:l?.onRender}},render(){const{mergedClsPrefix:e}=this;return this.onRender?.(),u(),I("div",{class:N([`${e}-layout-header`,this.themeClass,this.position&&`${e}-layout-header--${this.position}-positioned`,this.bordered&&`${e}-layout-header--bordered`]),style:Y(this.cssVars)},[R(()=>this.$slots.default?.())],6)}}),lt=h("layout-sider",`
 flex-shrink: 0;
 box-sizing: border-box;
 position: relative;
 z-index: 1;
 color: var(--n-text-color);
 transition:
 color .3s var(--n-bezier),
 border-color .3s var(--n-bezier),
 min-width .3s var(--n-bezier),
 max-width .3s var(--n-bezier),
 transform .3s var(--n-bezier),
 background-color .3s var(--n-bezier);
 background-color: var(--n-color);
 display: flex;
 justify-content: flex-end;
`,[T("bordered",[v("border",`
 content: "";
 position: absolute;
 top: 0;
 bottom: 0;
 width: 1px;
 background-color: var(--n-border-color);
 transition: background-color .3s var(--n-bezier);
 `)]),v("left-placement",[T("bordered",[v("border",`
 right: 0;
 `)])]),T("right-placement",`
 justify-content: flex-start;
 `,[T("bordered",[v("border",`
 left: 0;
 `)]),T("collapsed",[h("layout-toggle-button",[h("base-icon",`
 transform: rotate(180deg);
 `)]),h("layout-toggle-bar",[z("&:hover",[v("top",{transform:"rotate(-12deg) scale(1.15) translateY(-2px)"}),v("bottom",{transform:"rotate(12deg) scale(1.15) translateY(2px)"})])])]),h("layout-toggle-button",`
 left: 0;
 transform: translateX(-50%) translateY(-50%);
 `,[h("base-icon",`
 transform: rotate(0);
 `)]),h("layout-toggle-bar",`
 left: -28px;
 transform: rotate(180deg);
 `,[z("&:hover",[v("top",{transform:"rotate(12deg) scale(1.15) translateY(-2px)"}),v("bottom",{transform:"rotate(-12deg) scale(1.15) translateY(2px)"})])])]),T("collapsed",[h("layout-toggle-bar",[z("&:hover",[v("top",{transform:"rotate(-12deg) scale(1.15) translateY(-2px)"}),v("bottom",{transform:"rotate(12deg) scale(1.15) translateY(2px)"})])]),h("layout-toggle-button",[h("base-icon",`
 transform: rotate(0);
 `)])]),h("layout-toggle-button",`
 transition:
 color .3s var(--n-bezier),
 right .3s var(--n-bezier),
 left .3s var(--n-bezier),
 border-color .3s var(--n-bezier),
 background-color .3s var(--n-bezier);
 cursor: pointer;
 width: 24px;
 height: 24px;
 position: absolute;
 top: 50%;
 right: 0;
 border-radius: 50%;
 display: flex;
 align-items: center;
 justify-content: center;
 font-size: 18px;
 color: var(--n-toggle-button-icon-color);
 border: var(--n-toggle-button-border);
 background-color: var(--n-toggle-button-color);
 box-shadow: 0 2px 4px 0px rgba(0, 0, 0, .06);
 transform: translateX(50%) translateY(-50%);
 z-index: 1;
 `,[h("base-icon",`
 transition: transform .3s var(--n-bezier);
 transform: rotate(180deg);
 `)]),h("layout-toggle-bar",`
 cursor: pointer;
 height: 72px;
 width: 32px;
 position: absolute;
 top: calc(50% - 36px);
 right: -28px;
 `,[v("top, bottom",`
 position: absolute;
 width: 4px;
 border-radius: 2px;
 height: 38px;
 left: 14px;
 transition: 
 background-color .3s var(--n-bezier),
 transform .3s var(--n-bezier);
 `),v("bottom",`
 position: absolute;
 top: 34px;
 `),z("&:hover",[v("top",{transform:"rotate(12deg) scale(1.15) translateY(-2px)"}),v("bottom",{transform:"rotate(-12deg) scale(1.15) translateY(2px)"})]),v("top, bottom",{backgroundColor:"var(--n-toggle-bar-color)"}),z("&:hover",[v("top, bottom",{backgroundColor:"var(--n-toggle-bar-color-hover)"})])]),v("border",`
 position: absolute;
 top: 0;
 right: 0;
 bottom: 0;
 width: 1px;
 transition: background-color .3s var(--n-bezier);
 `),h("layout-sider-scroll-container",`
 flex-grow: 1;
 flex-shrink: 0;
 box-sizing: border-box;
 height: 100%;
 opacity: 0;
 transition: opacity .3s var(--n-bezier);
 max-width: 100%;
 `),T("show-content",[h("layout-sider-scroll-container",{opacity:1})]),T("absolute-positioned",`
 position: absolute;
 left: 0;
 top: 0;
 bottom: 0;
 `)]);const it=["onClick"];var at=j({props:{clsPrefix:{type:String,required:!0},onClick:Function},render(){const{clsPrefix:e}=this;return u(),I("div",{onClick:this.onClick,class:N(`${e}-layout-toggle-bar`)},[ee("div",{class:N(`${e}-layout-toggle-bar__top`)},null,2),ee("div",{class:N(`${e}-layout-toggle-bar__bottom`)},null,2)],10,it)}});const st=["onClick"];var ct=j({name:"LayoutToggleButton",props:{clsPrefix:{type:String,required:!0},onClick:Function},render(){const{clsPrefix:e}=this;return u(),I("div",{class:N(`${e}-layout-toggle-button`),onClick:this.onClick},[(u(),E(qe,{clsPrefix:e},{default:()=>(u(),E($o))},1032,["clsPrefix"]))],10,st)}});const dt=["onTransitionend"],ut={position:Ne,bordered:Boolean,collapsedWidth:{type:Number,default:48},width:{type:[Number,String],default:272},contentClass:String,contentStyle:{type:[String,Object],default:""},collapseMode:{type:String,default:"transform"},collapsed:{type:Boolean,default:void 0},defaultCollapsed:Boolean,showCollapsedContent:{type:Boolean,default:!0},showTrigger:{type:[Boolean,String],default:!1},nativeScrollbar:{type:Boolean,default:!0},inverted:Boolean,scrollbarProps:Object,triggerClass:String,triggerStyle:[String,Object],collapsedTriggerClass:String,collapsedTriggerStyle:[String,Object],"onUpdate:collapsed":[Function,Array],onUpdateCollapsed:[Function,Array],onAfterEnter:Function,onAfterLeave:Function,onExpand:[Function,Array],onCollapse:[Function,Array],onScroll:Function};var vt=j({name:"LayoutSider",props:{...Z.props,...ut},setup(e){const o=X(Ze),r=M(null),i=M(null),a=M(e.defaultCollapsed),l=Re(ge(e,"collapsed"),a),d=f(()=>ye(l.value?e.collapsedWidth:e.width)),c=f(()=>e.collapseMode!=="transform"?{}:{minWidth:ye(e.width)}),s=f(()=>o?o.siderPlacement:"left");function m(w,x){if(e.nativeScrollbar){const{value:O}=r;O&&(x===void 0?O.scrollTo(w):O.scrollTo(w,x))}else{const{value:O}=i;O&&O.scrollTo(w,x)}}function S(){const{"onUpdate:collapsed":w,onUpdateCollapsed:x,onExpand:O,onCollapse:D}=e,{value:U}=l;x&&q(x,!U),w&&q(w,!U),a.value=!U,U?O&&q(O):D&&q(D)}let C=0,b=0;const P=w=>{const x=w.target;C=x.scrollLeft,b=x.scrollTop,e.onScroll?.(w)};We(()=>{if(e.nativeScrollbar){const w=r.value;w&&(w.scrollTop=b,w.scrollLeft=C)}}),re(Xe,{collapsedRef:l,collapseModeRef:ge(e,"collapseMode")});const{mergedClsPrefixRef:H,inlineThemeDisabled:A}=ce(e),L=Z("Layout","-layout-sider",lt,Ae,e,H);function F(w){w.propertyName==="max-width"&&(l.value?e.onAfterLeave?.():e.onAfterEnter?.())}const p={scrollTo:m},k=f(()=>{const{common:{cubicBezierEaseInOut:w},self:x}=L.value,{siderToggleButtonColor:O,siderToggleButtonBorder:D,siderToggleBarColor:U,siderToggleBarColorHover:le}=x,K={"--n-bezier":w,"--n-toggle-button-color":O,"--n-toggle-button-border":D,"--n-toggle-bar-color":U,"--n-toggle-bar-color-hover":le};return e.inverted?(K["--n-color"]=x.siderColorInverted,K["--n-text-color"]=x.textColorInverted,K["--n-border-color"]=x.siderBorderColorInverted,K["--n-toggle-button-icon-color"]=x.siderToggleButtonIconColorInverted,K.__invertScrollbar=x.__invertScrollbar):(K["--n-color"]=x.siderColor,K["--n-text-color"]=x.textColor,K["--n-border-color"]=x.siderBorderColor,K["--n-toggle-button-icon-color"]=x.siderToggleButtonIconColor),K}),$=A?de("layout-sider",f(()=>e.inverted?"a":"b"),k,e):void 0;return{scrollableElRef:r,scrollbarInstRef:i,mergedClsPrefix:H,mergedTheme:L,styleMaxWidth:d,mergedCollapsed:l,scrollContainerStyle:c,siderPlacement:s,handleNativeElScroll:P,handleTransitionend:F,handleTriggerClick:S,inlineThemeDisabled:A,cssVars:k,themeClass:$?.themeClass,onRender:$?.onRender,...p}},render(){const{mergedClsPrefix:e,mergedCollapsed:o,showTrigger:r}=this;return this.onRender?.(),u(),I("aside",{class:N([`${e}-layout-sider`,this.themeClass,`${e}-layout-sider--${this.position}-positioned`,`${e}-layout-sider--${this.siderPlacement}-placement`,this.bordered&&`${e}-layout-sider--bordered`,o&&`${e}-layout-sider--collapsed`,(!o||this.showCollapsedContent)&&`${e}-layout-sider--show-content`]),onTransitionend:this.handleTransitionend,style:Y([this.inlineThemeDisabled?void 0:this.cssVars,{maxWidth:this.styleMaxWidth,width:ye(this.width)}])},[this.nativeScrollbar?(u(),I("div",{key:1,class:N([`${e}-layout-sider-scroll-container`,this.contentClass]),onScroll:this.handleNativeElScroll,style:Y([this.scrollContainerStyle,{overflow:"auto"},this.contentStyle]),ref:"scrollableElRef"},[R(()=>this.$slots.default?.())],46,["onScroll"])):(u(),E(Ge,J({key:0},this.scrollbarProps,{onScroll:this.onScroll,ref:"scrollbarInstRef",style:this.scrollContainerStyle,contentStyle:this.contentStyle,contentClass:this.contentClass,theme:this.mergedTheme.peers.Scrollbar,themeOverrides:this.mergedTheme.peerOverrides.Scrollbar,builtinThemeOverrides:this.inverted&&this.cssVars.__invertScrollbar==="true"?{colorHover:"rgba(255, 255, 255, .4)",color:"rgba(255, 255, 255, .3)"}:void 0}),Ue(this.$slots),1040,["onScroll","style","contentStyle","contentClass","theme","themeOverrides","builtinThemeOverrides"])),r?(u(),I(Q,{key:2},[r==="bar"?(u(),E(at,{key:0,clsPrefix:e,class:N(o?this.collapsedTriggerClass:this.triggerClass),style:Y(o?this.collapsedTriggerStyle:this.triggerStyle),onClick:this.handleTriggerClick},null,8,["clsPrefix","class","style","onClick"])):(u(),E(ct,{key:1,clsPrefix:e,class:N(o?this.collapsedTriggerClass:this.triggerClass),style:Y(o?this.collapsedTriggerStyle:this.triggerStyle),onClick:this.handleTriggerClick},null,8,["clsPrefix","class","style","onClick"]))],64)):R(()=>null),this.bordered?(u(),I("div",{key:4,class:N(`${e}-layout-sider__border`)},null,2)):R(()=>null)],46,dt)}});const ue=ne("n-menu"),Je=ne("n-submenu"),He=ne("n-menu-item-group"),Ve=[z("&::before","background-color: var(--n-item-color-hover);"),v("arrow",`
 color: var(--n-arrow-color-hover);
 `),v("icon",`
 color: var(--n-item-icon-color-hover);
 `),h("menu-item-content-header",`
 color: var(--n-item-text-color-hover);
 `,[z("a",`
 color: var(--n-item-text-color-hover);
 `),v("extra",`
 color: var(--n-item-text-color-hover);
 `)])],je=[v("icon",`
 color: var(--n-item-icon-color-hover-horizontal);
 `),h("menu-item-content-header",`
 color: var(--n-item-text-color-hover-horizontal);
 `,[z("a",`
 color: var(--n-item-text-color-hover-horizontal);
 `),v("extra",`
 color: var(--n-item-text-color-hover-horizontal);
 `)])];var ht=z([h("menu",`
 background-color: var(--n-color);
 color: var(--n-item-text-color);
 overflow: hidden;
 transition: background-color .3s var(--n-bezier);
 box-sizing: border-box;
 font-size: var(--n-font-size);
 padding-bottom: 6px;
 `,[T("horizontal",`
 max-width: 100%;
 width: 100%;
 display: flex;
 overflow: hidden;
 padding-bottom: 0;
 `,[h("submenu","margin: 0;"),h("menu-item","margin: 0;"),h("menu-item-content",`
 padding: 0 20px;
 border-bottom: 2px solid #0000;
 `,[z("&::before","display: none;"),T("selected","border-bottom: 2px solid var(--n-border-color-horizontal)")]),h("menu-item-content",[T("selected",[v("icon","color: var(--n-item-icon-color-active-horizontal);"),h("menu-item-content-header",`
 color: var(--n-item-text-color-active-horizontal);
 `,[z("a","color: var(--n-item-text-color-active-horizontal);"),v("extra","color: var(--n-item-text-color-active-horizontal);")])]),T("child-active",`
 border-bottom: 2px solid var(--n-border-color-horizontal);
 `,[h("menu-item-content-header",`
 color: var(--n-item-text-color-child-active-horizontal);
 `,[z("a",`
 color: var(--n-item-text-color-child-active-horizontal);
 `),v("extra",`
 color: var(--n-item-text-color-child-active-horizontal);
 `)]),v("icon",`
 color: var(--n-item-icon-color-child-active-horizontal);
 `)]),ae("disabled",[ae("selected, child-active",[z("&:focus-within",je)]),T("selected",[oe(null,[v("icon","color: var(--n-item-icon-color-active-hover-horizontal);"),h("menu-item-content-header",`
 color: var(--n-item-text-color-active-hover-horizontal);
 `,[z("a","color: var(--n-item-text-color-active-hover-horizontal);"),v("extra","color: var(--n-item-text-color-active-hover-horizontal);")])])]),T("child-active",[oe(null,[v("icon","color: var(--n-item-icon-color-child-active-hover-horizontal);"),h("menu-item-content-header",`
 color: var(--n-item-text-color-child-active-hover-horizontal);
 `,[z("a","color: var(--n-item-text-color-child-active-hover-horizontal);"),v("extra","color: var(--n-item-text-color-child-active-hover-horizontal);")])])]),oe("border-bottom: 2px solid var(--n-border-color-horizontal);",je)]),h("menu-item-content-header",[z("a","color: var(--n-item-text-color-horizontal);")])])]),ae("responsive",[h("menu-item-content-header",`
 overflow: hidden;
 text-overflow: ellipsis;
 `)]),T("collapsed",[h("menu-item-content",[T("selected",[z("&::before",`
 background-color: var(--n-item-color-active-collapsed) !important;
 `)]),h("menu-item-content-header","opacity: 0;"),v("arrow","opacity: 0;"),v("icon","color: var(--n-item-icon-color-collapsed);")])]),h("menu-item",`
 height: var(--n-item-height);
 margin-top: 6px;
 position: relative;
 `),h("menu-item-content",`
 box-sizing: border-box;
 line-height: 1.75;
 height: 100%;
 display: grid;
 grid-template-areas: "icon content arrow";
 grid-template-columns: auto 1fr auto;
 align-items: center;
 cursor: pointer;
 position: relative;
 padding-right: 18px;
 transition:
 background-color .3s var(--n-bezier),
 padding-left .3s var(--n-bezier),
 border-color .3s var(--n-bezier);
 `,[z("> *","z-index: 1;"),z("&::before",`
 z-index: auto;
 content: "";
 background-color: #0000;
 position: absolute;
 left: 8px;
 right: 8px;
 top: 0;
 bottom: 0;
 pointer-events: none;
 border-radius: var(--n-border-radius);
 transition: background-color .3s var(--n-bezier);
 `),T("disabled",`
 opacity: .45;
 cursor: not-allowed;
 `),T("collapsed",[v("arrow","transform: rotate(0);")]),T("selected",[z("&::before","background-color: var(--n-item-color-active);"),v("arrow","color: var(--n-arrow-color-active);"),v("icon","color: var(--n-item-icon-color-active);"),h("menu-item-content-header",`
 color: var(--n-item-text-color-active);
 `,[z("a","color: var(--n-item-text-color-active);"),v("extra","color: var(--n-item-text-color-active);")])]),T("child-active",[h("menu-item-content-header",`
 color: var(--n-item-text-color-child-active);
 `,[z("a",`
 color: var(--n-item-text-color-child-active);
 `),v("extra",`
 color: var(--n-item-text-color-child-active);
 `)]),v("arrow",`
 color: var(--n-arrow-color-child-active);
 `),v("icon",`
 color: var(--n-item-icon-color-child-active);
 `)]),ae("disabled",[ae("selected, child-active",[z("&:focus-within",Ve)]),T("selected",[oe(null,[v("arrow","color: var(--n-arrow-color-active-hover);"),v("icon","color: var(--n-item-icon-color-active-hover);"),h("menu-item-content-header",`
 color: var(--n-item-text-color-active-hover);
 `,[z("a","color: var(--n-item-text-color-active-hover);"),v("extra","color: var(--n-item-text-color-active-hover);")])])]),T("child-active",[oe(null,[v("arrow","color: var(--n-arrow-color-child-active-hover);"),v("icon","color: var(--n-item-icon-color-child-active-hover);"),h("menu-item-content-header",`
 color: var(--n-item-text-color-child-active-hover);
 `,[z("a","color: var(--n-item-text-color-child-active-hover);"),v("extra","color: var(--n-item-text-color-child-active-hover);")])])]),T("selected",[oe(null,[z("&::before","background-color: var(--n-item-color-active-hover);")])]),oe(null,Ve)]),v("icon",`
 grid-area: icon;
 color: var(--n-item-icon-color);
 transition:
 color .3s var(--n-bezier),
 font-size .3s var(--n-bezier),
 margin-right .3s var(--n-bezier);
 box-sizing: content-box;
 display: inline-flex;
 align-items: center;
 justify-content: center;
 `),v("arrow",`
 grid-area: arrow;
 font-size: 16px;
 color: var(--n-arrow-color);
 transform: rotate(180deg);
 opacity: 1;
 transition:
 color .3s var(--n-bezier),
 transform 0.2s var(--n-bezier),
 opacity 0.2s var(--n-bezier);
 `),h("menu-item-content-header",`
 grid-area: content;
 transition:
 color .3s var(--n-bezier),
 opacity .3s var(--n-bezier);
 opacity: 1;
 white-space: nowrap;
 color: var(--n-item-text-color);
 `,[z("a",`
 outline: none;
 text-decoration: none;
 transition: color .3s var(--n-bezier);
 color: var(--n-item-text-color);
 `,[z("&::before",`
 content: "";
 position: absolute;
 left: 0;
 right: 0;
 top: 0;
 bottom: 0;
 `)]),v("extra",`
 font-size: .93em;
 color: var(--n-group-text-color);
 transition: color .3s var(--n-bezier);
 `)])]),h("submenu",`
 cursor: pointer;
 position: relative;
 margin-top: 6px;
 `,[h("menu-item-content",`
 height: var(--n-item-height);
 `),h("submenu-children",`
 overflow: hidden;
 padding: 0;
 `,[ko({duration:".2s"})])]),h("menu-item-group",[h("menu-item-group-title",`
 margin-top: 6px;
 color: var(--n-group-text-color);
 cursor: default;
 font-size: .93em;
 height: 36px;
 display: flex;
 align-items: center;
 transition:
 padding-left .3s var(--n-bezier),
 color .3s var(--n-bezier);
 `)])]),h("menu-tooltip",[z("a",`
 color: inherit;
 text-decoration: none;
 `)]),h("menu-divider",`
 transition: background-color .3s var(--n-bezier);
 background-color: var(--n-divider-color);
 height: 1px;
 margin: 6px 18px;
 `)]);function oe(e,o){return[T("hover",e,o),z("&:hover",e,o)]}var mt=j({name:"MenuDivider",setup(){const{mergedClsPrefixRef:e,isHorizontalRef:o}=X(ue);return()=>o.value?null:(u(),I("div",{key:1,class:N(`${e.value}-menu-divider`)},null,2))}}),ft=j({name:"ChevronDownFilled",render(){return(()=>{const e=_e("f3af82a2aab086a5");return e[0]||(e[0]=ee("svg",{viewBox:"0 0 16 16",fill:"none",xmlns:"http://www.w3.org/2000/svg"},[ee("path",{d:"M3.20041 5.73966C3.48226 5.43613 3.95681 5.41856 4.26034 5.70041L8 9.22652L11.7397 5.70041C12.0432 5.41856 12.5177 5.43613 12.7996 5.73966C13.0815 6.0432 13.0639 6.51775 12.7603 6.7996L8.51034 10.7996C8.22258 11.0668 7.77743 11.0668 7.48967 10.7996L3.23966 6.7996C2.93613 6.51775 2.91856 6.0432 3.20041 5.73966Z",fill:"currentColor"})],-1))})()}});const gt=["onClick"];var eo=j({name:"MenuOptionContent",props:{collapsed:Boolean,disabled:Boolean,title:[String,Function],icon:Function,extra:[String,Function],showArrow:Boolean,childActive:Boolean,hover:Boolean,paddingLeft:Number,selected:Boolean,maxIconSize:{type:Number,required:!0},activeIconSize:{type:Number,required:!0},iconMarginRight:{type:Number,required:!0},clsPrefix:{type:String,required:!0},onClick:Function,tmNode:{type:Object,required:!0},isEllipsisPlaceholder:Boolean},setup(e){const{props:o}=X(ue);return{menuProps:o,style:f(()=>{const{paddingLeft:r}=e;return{paddingLeft:r&&`${r}px`}}),iconStyle:f(()=>{const{maxIconSize:r,activeIconSize:i,iconMarginRight:a}=e;return{width:`${r}px`,height:`${r}px`,fontSize:`${i}px`,marginRight:`${a}px`}})}},render(){const{clsPrefix:e,tmNode:o,menuProps:{renderIcon:r,renderLabel:i,renderExtra:a,expandIcon:l}}=this,d=r?r(o.rawNode):te(this.icon);return(()=>{const c=_e("7bb10afc6caf8fa4");return u(),I("div",{onClick:s=>{this.onClick?.(s)},role:"none",class:N([`${e}-menu-item-content`,{[`${e}-menu-item-content--selected`]:this.selected,[`${e}-menu-item-content--collapsed`]:this.collapsed,[`${e}-menu-item-content--child-active`]:this.childActive,[`${e}-menu-item-content--disabled`]:this.disabled,[`${e}-menu-item-content--hover`]:this.hover}]),style:Y(this.style)},[R(()=>d&&(u(),I("div",{class:N(`${e}-menu-item-content__icon`),style:Y(this.iconStyle),role:"none"},[R(()=>[d])],6))),ee("div",{class:N(`${e}-menu-item-content-header`),role:"none"},[this.isEllipsisPlaceholder?(u(),I(Q,{key:0},[R(()=>this.title)],64)):(u(),I(Q,{key:1},[i?(u(),I(Q,{key:0},[R(()=>i(o.rawNode))],64)):(u(),I(Q,{key:1},[R(()=>te(this.title))],64))],64)),this.extra||a?(u(),I("span",{key:2,class:N(`${e}-menu-item-content-header__extra`)},[c[0]||(c[0]=R(" ",-1)),a?(u(),I(Q,{key:0},[R(()=>a(o.rawNode))],64)):(u(),I(Q,{key:1},[R(()=>te(this.extra))],64))],2)):R(()=>null)],2),this.showArrow?(u(),E(qe,{key:0,ariaHidden:!0,class:N(`${e}-menu-item-content__arrow`),clsPrefix:e},{default:()=>l?l(o.rawNode):(u(),E(ft,{key:1}))},1032,["class","clsPrefix"])):R(()=>null)],14,gt)})()}});const fe=8;function Ee(e){const o=X(ue),{props:r,mergedCollapsedRef:i}=o,a=X(Je,null),l=X(He,null),d=f(()=>r.mode==="horizontal"),c=f(()=>d.value?r.dropdownPlacement:"tmNodes"in e?"right-start":"right"),s=f(()=>Math.max(r.collapsedIconSize??r.iconSize,r.iconSize));return{dropdownPlacement:c,activeIconSize:f(()=>!d.value&&e.root&&i.value?r.collapsedIconSize??r.iconSize:r.iconSize),maxIconSize:s,paddingLeft:f(()=>{if(d.value)return;const{collapsedWidth:m,indent:S,rootIndent:C}=r,{root:b,isGroup:P}=e,H=C===void 0?S:C;return b?i.value?m/2-s.value/2:H:l&&typeof l.paddingLeftRef.value=="number"?i.value?m/2-s.value/2:S/2+l.paddingLeftRef.value:a&&typeof a.paddingLeftRef.value=="number"?(P?S/2:S)+a.paddingLeftRef.value:0}),iconMarginRight:f(()=>{const{collapsedWidth:m,indent:S,rootIndent:C}=r,{value:b}=s,{root:P}=e;return d.value||!P||!i.value?fe:(C===void 0?S:C)+b+fe-(m+b)/2}),NMenu:o,NSubmenu:a,NMenuOptionGroup:l}}const Le={internalKey:{type:[String,Number],required:!0},root:Boolean,isGroup:Boolean,level:{type:Number,required:!0},title:[String,Function],extra:[String,Function]},oo={...Le,tmNode:{type:Object,required:!0},disabled:Boolean,icon:Function,onClick:Function},pt=Te(oo),bt=j({name:"MenuOption",props:oo,setup(e){const o=Ee(e),{NSubmenu:r,NMenu:i,NMenuOptionGroup:a}=o,{props:l,mergedClsPrefixRef:d,mergedCollapsedRef:c}=i,s=r?r.mergedDisabledRef:a?a.mergedDisabledRef:{value:!1},m=f(()=>s.value||e.disabled);function S(b){const{onClick:P}=e;P&&P(b)}function C(b){m.value||(i.doSelect(e.internalKey,e.tmNode.rawNode),S(b))}return{mergedClsPrefix:d,dropdownPlacement:o.dropdownPlacement,paddingLeft:o.paddingLeft,iconMarginRight:o.iconMarginRight,maxIconSize:o.maxIconSize,activeIconSize:o.activeIconSize,mergedTheme:i.mergedThemeRef,menuProps:l,dropdownEnabled:Ie(()=>e.root&&c.value&&l.mode!=="horizontal"&&!m.value),selected:Ie(()=>i.mergedValueRef.value===e.internalKey),mergedDisabled:m,handleClick:C}},render(){const{mergedClsPrefix:e,mergedTheme:o,tmNode:r,menuProps:{renderLabel:i,nodeProps:a}}=this,l=a?.(r.rawNode);return u(),I("div",J(l,{role:"menuitem",class:[`${e}-menu-item`,l?.class]}),[(u(),E(Mo,{theme:o.peers.Tooltip,themeOverrides:o.peerOverrides.Tooltip,trigger:"hover",placement:this.dropdownPlacement,disabled:!this.dropdownEnabled||this.title===void 0,internalExtraClass:["menu-tooltip"]},{default:()=>i?i(r.rawNode):te(this.title),trigger:()=>(u(),E(eo,{tmNode:r,clsPrefix:e,paddingLeft:this.paddingLeft,iconMarginRight:this.iconMarginRight,maxIconSize:this.maxIconSize,activeIconSize:this.activeIconSize,selected:this.selected,title:this.title,extra:this.extra,disabled:this.mergedDisabled,icon:this.icon,onClick:this.handleClick},null,8,["tmNode","clsPrefix","paddingLeft","iconMarginRight","maxIconSize","activeIconSize","selected","title","extra","disabled","icon","onClick"]))},1032,["theme","themeOverrides","placement","disabled"]))],16)}}),to={...Le,tmNode:{type:Object,required:!0},tmNodes:{type:Array,required:!0}},xt=Te(to),yt=j({name:"MenuOptionGroup",props:to,setup(e){const o=Ee(e),{NSubmenu:r}=o,i=f(()=>r?.mergedDisabledRef.value?!0:e.tmNode.disabled);re(He,{paddingLeftRef:o.paddingLeft,mergedDisabledRef:i});const{mergedClsPrefixRef:a,props:l}=X(ue);return function(){const{value:d}=a,c=o.paddingLeft.value,{nodeProps:s}=l,m=s?.(e.tmNode.rawNode);return(()=>{const S=_e("45eca6a63be5028b");return u(),I("div",{class:N(`${d}-menu-item-group`),role:"group"},[ee("div",J(m,{class:[`${d}-menu-item-group-title`,m?.class],style:[m?.style||"",c!==void 0?`padding-left: ${c}px;`:""]}),[R(()=>te(e.title)),e.extra?(u(),I(Q,{key:0},[S[0]||(S[0]=R(" ",-1)),R(()=>te(e.extra))],64)):R(()=>null)],16),ee("div",null,[R(()=>e.tmNodes.map(C=>Be(C,l)))])],2)})()}}}),Ct=["aria-expanded","id"],zt=["aria-expanded","id"],ro={...Le,rawNodes:{type:Array,default:()=>[]},tmNodes:{type:Array,default:()=>[]},tmNode:{type:Object,required:!0},disabled:Boolean,icon:Function,onClick:Function,domId:String,virtualChildActive:{type:Boolean,default:void 0},isEllipsisPlaceholder:Boolean},St=Te(ro),Pe=j({name:"Submenu",props:ro,setup(e){const o=Ee(e),{NMenu:r,NSubmenu:i}=o,{props:a,mergedCollapsedRef:l,mergedThemeRef:d}=r,c=f(()=>{const{disabled:b}=e;return i?.mergedDisabledRef.value||a.disabled?!0:b}),s=M(!1);re(Je,{paddingLeftRef:o.paddingLeft,mergedDisabledRef:c}),re(He,null);function m(){const{onClick:b}=e;b&&b()}function S(){c.value||(l.value||r.toggleExpand(e.internalKey),m())}function C(b){s.value=b}return{menuProps:a,mergedTheme:d,doSelect:r.doSelect,inverted:r.invertedRef,isHorizontal:r.isHorizontalRef,mergedClsPrefix:r.mergedClsPrefixRef,maxIconSize:o.maxIconSize,activeIconSize:o.activeIconSize,iconMarginRight:o.iconMarginRight,dropdownPlacement:o.dropdownPlacement,dropdownShow:s,paddingLeft:o.paddingLeft,mergedDisabled:c,mergedValue:r.mergedValueRef,childActive:Ie(()=>e.virtualChildActive??r.activePathRef.value.includes(e.internalKey)),collapsed:f(()=>a.mode==="horizontal"?!1:l.value?!0:!r.mergedExpandedKeysRef.value.includes(e.internalKey)),dropdownEnabled:f(()=>!c.value&&(a.mode==="horizontal"||l.value)),handlePopoverShowChange:C,handleClick:S}},render(){const{mergedClsPrefix:e,menuProps:{renderIcon:o,renderLabel:r}}=this,i=()=>{const{isHorizontal:l,paddingLeft:d,collapsed:c,mergedDisabled:s,maxIconSize:m,activeIconSize:S,title:C,childActive:b,icon:P,handleClick:H,menuProps:{nodeProps:A},dropdownShow:L,iconMarginRight:F,tmNode:p,mergedClsPrefix:k,isEllipsisPlaceholder:$,extra:w}=this,x=A?.(p.rawNode);return u(),I("div",J(x,{class:[`${k}-menu-item`,x?.class],role:"menuitem"}),[(u(),E(eo,{tmNode:p,paddingLeft:d,collapsed:c,disabled:s,iconMarginRight:F,maxIconSize:m,activeIconSize:S,title:C,extra:w,showArrow:!l,childActive:b,clsPrefix:k,icon:P,hover:L,onClick:H,isEllipsisPlaceholder:$},null,8,["tmNode","paddingLeft","collapsed","disabled","iconMarginRight","maxIconSize","activeIconSize","title","extra","showArrow","childActive","clsPrefix","icon","hover","onClick","isEllipsisPlaceholder"]))],16)},a=()=>(u(),E(_o,null,{default:()=>{const{tmNodes:l,collapsed:d}=this;return d?null:(u(),I("div",{key:1,class:N(`${e}-submenu-children`),role:"menu"},[R(()=>l.map(c=>Be(c,this.menuProps)))],2))}},1024));return this.root?(u(),E(Ye,J({key:2,size:"large",trigger:"hover"},this.menuProps?.dropdownProps,{themeOverrides:this.mergedTheme.peerOverrides.Dropdown,theme:this.mergedTheme.peers.Dropdown,builtinThemeOverrides:{fontSizeLarge:"14px",optionIconSizeLarge:"18px"},value:this.mergedValue,disabled:!this.dropdownEnabled,placement:this.dropdownPlacement,keyField:this.menuProps.keyField,labelField:this.menuProps.labelField,childrenField:this.menuProps.childrenField,onUpdateShow:this.handlePopoverShowChange,options:this.rawNodes,onSelect:this.doSelect,inverted:this.inverted,renderIcon:o,renderLabel:r}),{default:()=>(u(),I("div",{class:N(`${e}-submenu`),role:"menu","aria-expanded":!this.collapsed,id:this.domId},[R(()=>i()),this.isHorizontal?R(()=>null):(u(),I(Q,{key:1},[R(()=>a())],64))],10,Ct))},1040,["themeOverrides","theme","value","disabled","placement","keyField","labelField","childrenField","onUpdateShow","options","onSelect","inverted","renderIcon","renderLabel"])):(u(),I("div",{key:3,class:N(`${e}-submenu`),role:"menu","aria-expanded":!this.collapsed,id:this.domId},[R(()=>i()),R(()=>a())],10,zt))}});function ke(e){return e.type==="divider"||e.type==="render"}function wt(e){return e.type==="divider"}function Be(e,o){const{rawNode:r}=e,{show:i}=r;if(i===!1)return null;if(ke(r))return wt(r)?(u(),E(mt,J({key:e.key},r.props),null,16)):null;const{labelField:a}=o,{key:l,level:d,isGroup:c}=e,s={...r,title:r.title||r[a],extra:r.titleExtra||r.extra,key:l,internalKey:l,level:d,root:d===0,isGroup:c};return e.children?e.isGroup?se(yt,pe(s,xt,{tmNode:e,tmNodes:e.children,key:l})):se(Pe,pe(s,St,{key:l,rawNodes:r[o.childrenField],tmNodes:e.children,tmNode:e})):se(bt,pe(s,pt,{key:l,tmNode:e}))}const It={...Z.props,options:{type:Array,default:()=>[]},collapsed:{type:Boolean,default:void 0},collapsedWidth:{type:Number,default:48},iconSize:{type:Number,default:20},collapsedIconSize:{type:Number,default:24},rootIndent:Number,indent:{type:Number,default:32},labelField:{type:String,default:"label"},keyField:{type:String,default:"key"},childrenField:{type:String,default:"children"},disabledField:{type:String,default:"disabled"},defaultExpandAll:Boolean,defaultExpandedKeys:Array,expandedKeys:Array,value:[String,Number],defaultValue:{type:[String,Number],default:null},mode:{type:String,default:"vertical"},watchProps:{type:Array,default:void 0},disabled:Boolean,show:{type:Boolean,default:!0},inverted:Boolean,"onUpdate:expandedKeys":[Function,Array],onUpdateExpandedKeys:[Function,Array],onUpdateValue:[Function,Array],"onUpdate:value":[Function,Array],expandIcon:Function,renderIcon:Function,renderLabel:Function,renderExtra:Function,dropdownProps:Object,accordion:Boolean,nodeProps:Function,dropdownPlacement:{type:String,default:"bottom"},responsive:Boolean,items:Array,onOpenNamesChange:[Function,Array],onSelect:[Function,Array],onExpandedNamesChange:[Function,Array],expandedNames:Array,defaultExpandedNames:Array};var Rt=j({name:"Menu",inheritAttrs:!1,props:It,setup(e){const{mergedClsPrefixRef:o,inlineThemeDisabled:r}=ce(e),i=Z("Menu","-menu",ht,Ao,e,o),a=X(Xe,null),l=f(()=>{const{collapsed:g}=e;if(g!==void 0)return g;if(a){const{collapseModeRef:_,collapsedRef:t}=a;if(_.value==="width")return t.value??!1}return!1}),d=f(()=>{const{keyField:g,childrenField:_,disabledField:t}=e;return xe(e.items||e.options,{getIgnored(y){return ke(y)},getChildren(y){return y[_]},getDisabled(y){return y[t]},getKey(y){return y[g]??y.name}})}),c=f(()=>new Set(d.value.treeNodes.map(g=>g.key))),{watchProps:s}=e,m=M(null);s?.includes("defaultValue")?we(()=>{m.value=e.defaultValue}):m.value=e.defaultValue;const S=ge(e,"value"),C=Re(S,m),b=M([]),P=()=>{b.value=e.defaultExpandAll?d.value.getNonLeafKeys():e.defaultExpandedNames||e.defaultExpandedKeys||d.value.getPath(C.value,{includeSelf:!1}).keyPath};s?.includes("defaultExpandedKeys")?we(P):P();const H=jo(e,["expandedNames","expandedKeys"]),A=Re(H,b),L=f(()=>d.value.treeNodes),F=f(()=>d.value.getPath(C.value).keyPath);re(ue,{props:e,mergedCollapsedRef:l,mergedThemeRef:i,mergedValueRef:C,mergedExpandedKeysRef:A,activePathRef:F,mergedClsPrefixRef:o,isHorizontalRef:f(()=>e.mode==="horizontal"),invertedRef:ge(e,"inverted"),doSelect:p,toggleExpand:$});function p(g,_){const{"onUpdate:value":t,onUpdateValue:y,onSelect:G}=e;y&&q(y,g,_),t&&q(t,g,_),G&&q(G,g,_),m.value=g}function k(g){const{"onUpdate:expandedKeys":_,onUpdateExpandedKeys:t,onExpandedNamesChange:y,onOpenNamesChange:G}=e;_&&q(_,g),t&&q(t,g),y&&q(y,g),G&&q(G,g),b.value=g}function $(g){const _=Array.from(A.value),t=_.findIndex(y=>y===g);if(~t)_.splice(t,1);else{if(e.accordion&&c.value.has(g)){const y=_.findIndex(G=>c.value.has(G));y>-1&&_.splice(y,1)}_.push(g)}k(_)}const w=g=>{const _=d.value.getPath(g??C.value,{includeSelf:!1}).keyPath;if(!_.length)return;const t=Array.from(A.value),y=new Set([...t,..._]);e.accordion&&c.value.forEach(G=>{y.has(G)&&!_.includes(G)&&y.delete(G)}),k(Array.from(y))},x=f(()=>{const{inverted:g}=e,{common:{cubicBezierEaseInOut:_},self:t}=i.value,{borderRadius:y,borderColorHorizontal:G,fontSize:uo,itemHeight:vo,dividerColor:ho}=t,n={"--n-divider-color":ho,"--n-bezier":_,"--n-font-size":uo,"--n-border-color-horizontal":G,"--n-border-radius":y,"--n-item-height":vo};return g?(n["--n-group-text-color"]=t.groupTextColorInverted,n["--n-color"]=t.colorInverted,n["--n-item-text-color"]=t.itemTextColorInverted,n["--n-item-text-color-hover"]=t.itemTextColorHoverInverted,n["--n-item-text-color-active"]=t.itemTextColorActiveInverted,n["--n-item-text-color-child-active"]=t.itemTextColorChildActiveInverted,n["--n-item-text-color-child-active-hover"]=t.itemTextColorChildActiveInverted,n["--n-item-text-color-active-hover"]=t.itemTextColorActiveHoverInverted,n["--n-item-icon-color"]=t.itemIconColorInverted,n["--n-item-icon-color-hover"]=t.itemIconColorHoverInverted,n["--n-item-icon-color-active"]=t.itemIconColorActiveInverted,n["--n-item-icon-color-active-hover"]=t.itemIconColorActiveHoverInverted,n["--n-item-icon-color-child-active"]=t.itemIconColorChildActiveInverted,n["--n-item-icon-color-child-active-hover"]=t.itemIconColorChildActiveHoverInverted,n["--n-item-icon-color-collapsed"]=t.itemIconColorCollapsedInverted,n["--n-item-text-color-horizontal"]=t.itemTextColorHorizontalInverted,n["--n-item-text-color-hover-horizontal"]=t.itemTextColorHoverHorizontalInverted,n["--n-item-text-color-active-horizontal"]=t.itemTextColorActiveHorizontalInverted,n["--n-item-text-color-child-active-horizontal"]=t.itemTextColorChildActiveHorizontalInverted,n["--n-item-text-color-child-active-hover-horizontal"]=t.itemTextColorChildActiveHoverHorizontalInverted,n["--n-item-text-color-active-hover-horizontal"]=t.itemTextColorActiveHoverHorizontalInverted,n["--n-item-icon-color-horizontal"]=t.itemIconColorHorizontalInverted,n["--n-item-icon-color-hover-horizontal"]=t.itemIconColorHoverHorizontalInverted,n["--n-item-icon-color-active-horizontal"]=t.itemIconColorActiveHorizontalInverted,n["--n-item-icon-color-active-hover-horizontal"]=t.itemIconColorActiveHoverHorizontalInverted,n["--n-item-icon-color-child-active-horizontal"]=t.itemIconColorChildActiveHorizontalInverted,n["--n-item-icon-color-child-active-hover-horizontal"]=t.itemIconColorChildActiveHoverHorizontalInverted,n["--n-arrow-color"]=t.arrowColorInverted,n["--n-arrow-color-hover"]=t.arrowColorHoverInverted,n["--n-arrow-color-active"]=t.arrowColorActiveInverted,n["--n-arrow-color-active-hover"]=t.arrowColorActiveHoverInverted,n["--n-arrow-color-child-active"]=t.arrowColorChildActiveInverted,n["--n-arrow-color-child-active-hover"]=t.arrowColorChildActiveHoverInverted,n["--n-item-color-hover"]=t.itemColorHoverInverted,n["--n-item-color-active"]=t.itemColorActiveInverted,n["--n-item-color-active-hover"]=t.itemColorActiveHoverInverted,n["--n-item-color-active-collapsed"]=t.itemColorActiveCollapsedInverted):(n["--n-group-text-color"]=t.groupTextColor,n["--n-color"]=t.color,n["--n-item-text-color"]=t.itemTextColor,n["--n-item-text-color-hover"]=t.itemTextColorHover,n["--n-item-text-color-active"]=t.itemTextColorActive,n["--n-item-text-color-child-active"]=t.itemTextColorChildActive,n["--n-item-text-color-child-active-hover"]=t.itemTextColorChildActiveHover,n["--n-item-text-color-active-hover"]=t.itemTextColorActiveHover,n["--n-item-icon-color"]=t.itemIconColor,n["--n-item-icon-color-hover"]=t.itemIconColorHover,n["--n-item-icon-color-active"]=t.itemIconColorActive,n["--n-item-icon-color-active-hover"]=t.itemIconColorActiveHover,n["--n-item-icon-color-child-active"]=t.itemIconColorChildActive,n["--n-item-icon-color-child-active-hover"]=t.itemIconColorChildActiveHover,n["--n-item-icon-color-collapsed"]=t.itemIconColorCollapsed,n["--n-item-text-color-horizontal"]=t.itemTextColorHorizontal,n["--n-item-text-color-hover-horizontal"]=t.itemTextColorHoverHorizontal,n["--n-item-text-color-active-horizontal"]=t.itemTextColorActiveHorizontal,n["--n-item-text-color-child-active-horizontal"]=t.itemTextColorChildActiveHorizontal,n["--n-item-text-color-child-active-hover-horizontal"]=t.itemTextColorChildActiveHoverHorizontal,n["--n-item-text-color-active-hover-horizontal"]=t.itemTextColorActiveHoverHorizontal,n["--n-item-icon-color-horizontal"]=t.itemIconColorHorizontal,n["--n-item-icon-color-hover-horizontal"]=t.itemIconColorHoverHorizontal,n["--n-item-icon-color-active-horizontal"]=t.itemIconColorActiveHorizontal,n["--n-item-icon-color-active-hover-horizontal"]=t.itemIconColorActiveHoverHorizontal,n["--n-item-icon-color-child-active-horizontal"]=t.itemIconColorChildActiveHorizontal,n["--n-item-icon-color-child-active-hover-horizontal"]=t.itemIconColorChildActiveHoverHorizontal,n["--n-arrow-color"]=t.arrowColor,n["--n-arrow-color-hover"]=t.arrowColorHover,n["--n-arrow-color-active"]=t.arrowColorActive,n["--n-arrow-color-active-hover"]=t.arrowColorActiveHover,n["--n-arrow-color-child-active"]=t.arrowColorChildActive,n["--n-arrow-color-child-active-hover"]=t.arrowColorChildActiveHover,n["--n-item-color-hover"]=t.itemColorHover,n["--n-item-color-active"]=t.itemColorActive,n["--n-item-color-active-hover"]=t.itemColorActiveHover,n["--n-item-color-active-collapsed"]=t.itemColorActiveCollapsed),n}),O=r?de("menu",f(()=>e.inverted?"a":"b"),x,e):void 0,D=To(),U=M(null),le=M(null);let K=!0;const ve=()=>{K?K=!1:U.value?.sync({showAllItemsBeforeCalculate:!0})};function ie(){return document.getElementById(D)}const he=M(-1);function no(g){he.value=e.options.length-g}function lo(g){g||(he.value=-1)}const io=f(()=>{const g=he.value;return{children:g===-1?[]:e.options.slice(g)}}),ao=f(()=>{const{childrenField:g,disabledField:_,keyField:t}=e;return xe([io.value],{getIgnored(y){return ke(y)},getChildren(y){return y[g]},getDisabled(y){return y[_]},getKey(y){return y[t]??y.name}})}),so=f(()=>xe([{}]).treeNodes[0]);function co(){if(he.value===-1)return u(),E(Pe,{root:!0,level:0,key:"__ellpisisGroupPlaceholder__",internalKey:"__ellpisisGroupPlaceholder__",title:"···",tmNode:so.value,domId:D,isEllipsisPlaceholder:!0},null,8,["tmNode","domId"]);const g=ao.value.treeNodes[0],_=F.value,t=!!g.children?.some(y=>_.includes(y.key));return u(),E(Pe,{level:0,root:!0,key:"__ellpisisGroup__",internalKey:"__ellpisisGroup__",title:"···",virtualChildActive:t,tmNode:g,domId:D,rawNodes:g.rawNode.children||[],tmNodes:g.children||[],isEllipsisPlaceholder:!0},null,8,["virtualChildActive","tmNode","domId","rawNodes","tmNodes"])}return{mergedClsPrefix:o,controlledExpandedKeys:H,uncontrolledExpanededKeys:b,mergedExpandedKeys:A,uncontrolledValue:m,mergedValue:C,activePath:F,tmNodes:L,mergedTheme:i,mergedCollapsed:l,cssVars:r?void 0:x,themeClass:O?.themeClass,overflowRef:U,counterRef:le,updateCounter:()=>{},onResize:ve,onUpdateOverflow:lo,onUpdateCount:no,renderCounter:co,getCounter:ie,onRender:O?.onRender,showOption:w,deriveResponsiveState:ve}},render(){const{mergedClsPrefix:e,mode:o,themeClass:r,onRender:i}=this;i?.();const a=()=>this.tmNodes.map(c=>Be(c,this.$props)),l=o==="horizontal"&&this.responsive,d=()=>se("div",J(this.$attrs,{role:o==="horizontal"?"menubar":"menu",class:[`${e}-menu`,r,`${e}-menu--${o}`,l&&`${e}-menu--responsive`,this.mergedCollapsed&&`${e}-menu--collapsed`],style:this.cssVars}),l?(u(),E(Ko,{key:2,ref:"overflowRef",onUpdateOverflow:this.onUpdateOverflow,getCounter:this.getCounter,onUpdateCount:this.onUpdateCount,updateCounter:this.updateCounter,style:{width:"100%",display:"flex",overflow:"hidden"}},{default:a,counter:this.renderCounter},1032,["onUpdateOverflow","getCounter","onUpdateCount","updateCounter"])):a());return l?(u(),E(De,{key:3,onResize:this.onResize},{default:d},1032,["onResize"])):d()}});const Pt=No("app",{state:()=>({sidebarCollapsed:!1}),actions:{setSidebarCollapsed(e){this.sidebarCollapsed=e},toggleSidebar(){this.sidebarCollapsed=!this.sidebarCollapsed}}}),kt=j({__name:"AppLayout",setup(e){const o=Pt(),r=Ho(),i=Oo(),a=Bo(),l=[{label:"Dashboard",key:"dashboard"},{label:"Servers",key:"servers"}],d=[{label:"Sign out",key:"sign-out"}],c=f(()=>String(i.name??"dashboard")),s=f(()=>i.meta.title??"Gotham"),m=f(()=>r.user?.email??""),S=f(()=>(r.user?.email?.[0]??"?").toUpperCase());function C(P){a.push({name:String(P)})}async function b(P){P==="sign-out"&&(await r.logout(),await a.push({name:"login"}))}return(P,H)=>(u(),E(B(Ke),{class:"shell","has-sider":""},{default:V(()=>[W(B(vt),{bordered:"","collapse-mode":"width",collapsed:B(o).sidebarCollapsed,"collapsed-width":64,width:240,"show-trigger":"","onUpdate:collapsed":B(o).setSidebarCollapsed},{default:V(()=>[H[0]||(H[0]=ee("div",{class:"brand"},"Gotham",-1)),W(B(Rt),{value:c.value,options:l,collapsed:B(o).sidebarCollapsed,"collapsed-width":64,"collapsed-icon-size":20,"onUpdate:value":C},null,8,["value","collapsed"])]),_:1},8,["collapsed","onUpdate:collapsed"]),W(B(Ke),null,{default:V(()=>[W(B(nt),{class:"topbar",bordered:""},{default:V(()=>[W(B($e),{strong:""},{default:V(()=>[me(be(s.value),1)]),_:1}),W(B(Me),{align:"center"},{default:V(()=>[B(r).isAuthenticated?(u(),E(B(Ye),{key:0,trigger:"click",options:d,onSelect:b},{default:V(()=>[W(B(Fe),{quaternary:""},{default:V(()=>[W(B(Me),{align:"center",size:8},{default:V(()=>[W(B(Zo),{round:"",size:28,src:B(r).user?.avatar},{default:V(()=>[me(be(S.value),1)]),_:1},8,["src"]),W(B($e),{depth:"2"},{default:V(()=>[me(be(m.value),1)]),_:1})]),_:1})]),_:1})]),_:1})):(u(),E(B(Eo),{key:1,to:"/login"},{default:V(()=>[W(B(Fe),{quaternary:"",type:"primary"},{default:V(()=>[...H[1]||(H[1]=[me("Sign in",-1)])]),_:1})]),_:1}))]),_:1})]),_:1}),W(B(ot),{class:"content","content-style":"padding: 24px;"},{default:V(()=>[W(B(Lo))]),_:1})]),_:1})]),_:1}))}}),Ht=Vo(kt,[["__scopeId","data-v-14593436"]]);export{Ht as default};
