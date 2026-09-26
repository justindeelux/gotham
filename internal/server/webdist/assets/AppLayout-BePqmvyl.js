import{p as Te,N as se,O as go,P as ne,s as m,Q as bo,q as I,S as Co,U as h,d as j,z as Z,V as xo,W as yo,g as u,c as w,A,F as Y,D as _,G as de,X,H as ue,I as f,r as M,o as zo,J as Ae,Y as Io,Z as So,h as E,_ as Ge,$ as ce,M as wo,a0 as Ao,a1 as We,a2 as be,a3 as Ro,x as k,a4 as qe,a5 as Q,a6 as Ye,a7 as Xe,a8 as re,E as ee,a9 as Ze,aa as J,ab as q,ac as ge,ad as ae,ae as Ho,af as _e,ag as te,ah as Ne,ai as Re,aj as Po,ak as Ce,al as ko,am as To,u as _o,w as V,b as B,a as W,i as fe,t as xe,B as Me,R as No,an as Eo,f as Lo,e as Bo}from"./index-WH-fP4ir.js";import{t as Fo,d as $o,a as Oo,C as Mo,T as Ko,D as Je,V as Vo,c as ye,_ as jo}from"./_plugin-vue_export-helper-C1wAItGh.js";import{u as Do,t as Ke,S as Ve}from"./text-WavMF84k.js";import{f as ze,u as He}from"./format-length-DX1owb8R.js";function Uo(e){const{borderRadius:t,avatarColor:o,cardColor:r,fontSize:a,heightTiny:l,heightSmall:d,heightMedium:c,heightLarge:s,heightHuge:v,modalColor:z,popoverColor:b}=e;return{borderRadius:t,fontSize:a,border:`2px solid ${r}`,heightTiny:l,heightSmall:d,heightMedium:c,heightLarge:s,heightHuge:v,color:se(r,o),colorModal:se(z,o),colorPopover:se(b,o)}}const Go={common:Te,self:Uo},Wo=go&&"loading"in document.createElement("img");function qo(e={}){const{root:t=null}=e;return{hash:`${e.rootMargin||"0px 0px 0px 0px"}-${Array.isArray(e.threshold)?e.threshold.join(","):e.threshold??"0"}`,options:{...e,root:(typeof t=="string"?document.querySelector(t):t)||document.documentElement}}}const Ie=new WeakMap,Se=new WeakMap,we=new WeakMap,Yo=(e,t,o)=>{if(!e)return()=>{};const r=qo(t),{root:a}=r.options;let l;const d=Ie.get(a);d?l=d:(l=new Map,Ie.set(a,l));let c,s;l.has(r.hash)?(s=l.get(r.hash),s[1].has(e)||(c=s[0],s[1].add(e),c.observe(e))):(c=new IntersectionObserver(b=>{b.forEach(C=>{if(C.isIntersecting){const R=Se.get(C.target),N=we.get(C.target);R&&R(),N&&(N.value=!0)}})},r.options),c.observe(e),s=[c,new Set([e])],l.set(r.hash,s));let v=!1;const z=()=>{v||(Se.delete(e),we.delete(e),v=!0,s[1].has(e)&&(s[0].unobserve(e),s[1].delete(e)),s[1].size<=0&&l.delete(r.hash),l.size||Ie.delete(a))};return Se.set(e,z),we.set(e,o),z},Xo=ne("n-avatar-group");var Zo=m("avatar",`
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
`,[bo(I("&","--n-merged-color: var(--n-color-modal);")),Co(I("&","--n-merged-color: var(--n-color-popover);")),I("img",`
 width: 100%;
 height: 100%;
 `),h("text",`
 white-space: nowrap;
 display: inline-block;
 position: absolute;
 left: 50%;
 top: 50%;
 `),m("icon",`
 vertical-align: bottom;
 font-size: calc(var(--n-merged-size) - 6px);
 `),h("text","line-height: 1.25")]);const Jo=["src"],Qo={...Z.props,size:[String,Number],src:String,circle:{type:Boolean,default:void 0},objectFit:String,round:{type:Boolean,default:void 0},bordered:{type:Boolean,default:void 0},onError:Function,fallbackSrc:String,intersectionObserverOptions:Object,lazy:Boolean,onLoad:Function,renderPlaceholder:Function,renderFallback:Function,imgProps:Object,color:String};var et=j({name:"Avatar",props:Qo,slots:Object,setup(e){const{mergedClsPrefixRef:t,inlineThemeDisabled:o}=de(e),r=M(!1);let a=null;const l=M(null),d=M(null),c=()=>{const{value:g}=l;if(g&&(a===null||a!==g.innerHTML)){a=g.innerHTML;const{value:H}=d;if(H){const{offsetWidth:O,offsetHeight:S}=H,{offsetWidth:x,offsetHeight:F}=g,D=.9,U=Math.min(O/x*D,S/F*D,1);g.style.transform=`translateX(-50%) translateY(-50%) scale(${U})`}}},s=X(Xo,null),v=f(()=>{const{size:g}=e;if(g)return g;const{size:H}=s||{};return H||"medium"}),z=Z("Avatar","-avatar",Zo,Go,e,t),b=X(Fo,null),C=f(()=>{if(s)return!0;const{round:g,circle:H}=e;return g!==void 0||H!==void 0?g||H:b?b.roundRef.value:!1}),R=f(()=>s?!0:e.bordered||!1),N=f(()=>{const g=v.value,H=C.value,O=R.value,{color:S}=e,{self:{borderRadius:x,fontSize:F,color:D,border:U,colorModal:le,colorPopover:K},common:{cubicBezierEaseInOut:he}}=z.value;let ie;return typeof g=="number"?ie=`${g}px`:ie=z.value.self[wo("height",g)],{"--n-font-size":F,"--n-border":O?U:"none","--n-border-radius":H?"50%":x,"--n-color":S||D,"--n-color-modal":S||le,"--n-color-popover":S||K,"--n-bezier":he,"--n-merged-size":`var(--n-avatar-size-override, ${ie})`}}),T=o?ue("avatar",f(()=>{const g=v.value,H=C.value,O=R.value,{color:S}=e;let x="";return g&&(typeof g=="number"?x+=`a${g}`:x+=g[0]),H&&(x+="b"),O&&(x+="c"),S&&(x+=Ao(S)),x}),N,e):void 0,L=M(!e.lazy);zo(()=>{if(e.lazy&&e.intersectionObserverOptions){let g;const H=Ae(()=>{g?.(),g=void 0,e.lazy&&(g=Yo(d.value,e.intersectionObserverOptions,L))});Io(()=>{H(),g?.()})}}),So(()=>e.src||e.imgProps?.src,()=>{r.value=!1});const $=M(!e.lazy);return{textRef:l,selfRef:d,mergedRoundRef:C,mergedClsPrefix:t,fitTextTransform:c,cssVars:o?void 0:N,themeClass:T?.themeClass,onRender:T?.onRender,hasLoadError:r,shouldStartLoading:L,loaded:$,mergedOnError:g=>{if(!L.value)return;r.value=!0;const{onError:H,imgProps:{onError:O}={}}=e;H?.(g),O?.(g)},mergedOnLoad:g=>{const{onLoad:H,imgProps:{onLoad:O}={}}=e;H?.(g),O?.(g),$.value=!0}}},render(){const{$slots:e,src:t,mergedClsPrefix:o,lazy:r,onRender:a,loaded:l,hasLoadError:d,imgProps:c={}}=this;a?.();let s;const v=!l&&!d&&(this.renderPlaceholder?this.renderPlaceholder():this.$slots.placeholder?.());return this.hasLoadError?s=this.renderFallback?this.renderFallback():xo(e.fallback,()=>[(u(),w("img",{src:this.fallbackSrc,style:Y({objectFit:this.objectFit})},null,12,Jo))]):s=yo(e.default,z=>{if(z)return u(),E(Ge,{key:1,onResize:this.fitTextTransform},{default:()=>(u(),w("span",{ref:"textRef",class:_(`${o}-avatar__text`)},[A(()=>z)],2))},1032,["onResize"]);if(t||c.src){const b=this.src||c.src;return ce("img",{...c,loading:Wo&&!this.intersectionObserverOptions&&r?"lazy":"eager",src:r&&this.intersectionObserverOptions?this.shouldStartLoading?b:void 0:b,"data-image-src":b,onLoad:this.mergedOnLoad,onError:this.mergedOnError,style:[c.style||"",{objectFit:this.objectFit},v?{height:"0",width:"0",visibility:"hidden",position:"absolute"}:""]})}}),u(),w("span",{ref:"selfRef",class:_([`${o}-avatar`,this.themeClass]),style:Y(this.cssVars)},[A(()=>s),A(()=>r&&v)],6)}});function ot(e,t,o,r){return{itemColorHoverInverted:"#0000",itemColorActiveInverted:t,itemColorActiveHoverInverted:t,itemColorActiveCollapsedInverted:t,itemTextColorInverted:e,itemTextColorHoverInverted:o,itemTextColorChildActiveInverted:o,itemTextColorChildActiveHoverInverted:o,itemTextColorActiveInverted:o,itemTextColorActiveHoverInverted:o,itemTextColorHorizontalInverted:e,itemTextColorHoverHorizontalInverted:o,itemTextColorChildActiveHorizontalInverted:o,itemTextColorChildActiveHoverHorizontalInverted:o,itemTextColorActiveHorizontalInverted:o,itemTextColorActiveHoverHorizontalInverted:o,itemIconColorInverted:e,itemIconColorHoverInverted:o,itemIconColorActiveInverted:o,itemIconColorActiveHoverInverted:o,itemIconColorChildActiveInverted:o,itemIconColorChildActiveHoverInverted:o,itemIconColorCollapsedInverted:e,itemIconColorHorizontalInverted:e,itemIconColorHoverHorizontalInverted:o,itemIconColorActiveHorizontalInverted:o,itemIconColorActiveHoverHorizontalInverted:o,itemIconColorChildActiveHorizontalInverted:o,itemIconColorChildActiveHoverHorizontalInverted:o,arrowColorInverted:e,arrowColorHoverInverted:o,arrowColorActiveInverted:o,arrowColorActiveHoverInverted:o,arrowColorChildActiveInverted:o,arrowColorChildActiveHoverInverted:o,groupTextColorInverted:r}}function tt(e){const{borderRadius:t,textColor3:o,primaryColor:r,textColor2:a,textColor1:l,fontSize:d,dividerColor:c,hoverColor:s,primaryColorHover:v}=e;return{borderRadius:t,color:"#0000",groupTextColor:o,itemColorHover:s,itemColorActive:be(r,{alpha:.1}),itemColorActiveHover:be(r,{alpha:.1}),itemColorActiveCollapsed:be(r,{alpha:.1}),itemTextColor:a,itemTextColorHover:a,itemTextColorActive:r,itemTextColorActiveHover:r,itemTextColorChildActive:r,itemTextColorChildActiveHover:r,itemTextColorHorizontal:a,itemTextColorHoverHorizontal:v,itemTextColorActiveHorizontal:r,itemTextColorActiveHoverHorizontal:r,itemTextColorChildActiveHorizontal:r,itemTextColorChildActiveHoverHorizontal:r,itemIconColor:l,itemIconColorHover:l,itemIconColorActive:r,itemIconColorActiveHover:r,itemIconColorChildActive:r,itemIconColorChildActiveHover:r,itemIconColorCollapsed:l,itemIconColorHorizontal:l,itemIconColorHoverHorizontal:v,itemIconColorActiveHorizontal:r,itemIconColorActiveHoverHorizontal:r,itemIconColorChildActiveHorizontal:r,itemIconColorChildActiveHoverHorizontal:r,itemHeight:"42px",arrowColor:a,arrowColorHover:a,arrowColorActive:r,arrowColorActiveHover:r,arrowColorChildActive:r,arrowColorChildActiveHover:r,colorInverted:"#0000",borderColorHorizontal:"#0000",fontSize:d,dividerColor:c,...ot("#BBB",r,"#FFF","#AAA")}}const rt=We({name:"Menu",common:Te,peers:{Tooltip:Oo,Dropdown:$o},self:tt});function nt(e){const{baseColor:t,textColor2:o,bodyColor:r,cardColor:a,dividerColor:l,actionColor:d,scrollbarColor:c,scrollbarColorHover:s,invertedColor:v}=e;return{textColor:o,textColorInverted:"#FFF",color:r,colorEmbedded:d,headerColor:a,headerColorInverted:v,footerColor:d,footerColorInverted:v,headerBorderColor:l,headerBorderColorInverted:v,footerBorderColor:l,footerBorderColorInverted:v,siderBorderColor:l,siderBorderColorInverted:v,siderColor:a,siderColorInverted:v,siderToggleButtonBorder:`1px solid ${l}`,siderToggleButtonColor:t,siderToggleButtonIconColor:o,siderToggleButtonIconColorInverted:o,siderToggleBarColor:se(r,c),siderToggleBarColorHover:se(r,s),__invertScrollbar:"true"}}const Ee=We({name:"Layout",common:Te,peers:{Scrollbar:Ro},self:nt}),Qe=ne("n-layout-sider"),Le={type:String,default:"static"};var lt=m("layout",`
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
`,[m("layout-scroll-container",`
 overflow-x: hidden;
 box-sizing: border-box;
 height: 100%;
 `),k("absolute-positioned",`
 position: absolute;
 left: 0;
 right: 0;
 top: 0;
 bottom: 0;
 `)]);const it={embedded:Boolean,position:Le,nativeScrollbar:{type:Boolean,default:!0},scrollbarProps:Object,onScroll:Function,contentClass:String,contentStyle:{type:[String,Object],default:""},hasSider:Boolean,siderPlacement:{type:String,default:"left"}},eo=ne("n-layout");function oo(e){return j({name:e?"LayoutContent":"Layout",props:{...Z.props,...it},setup(t){const o=M(null),r=M(null),{mergedClsPrefixRef:a,inlineThemeDisabled:l}=de(t),d=Z("Layout","-layout",lt,Ee,t,a);function c(T,L){if(t.nativeScrollbar){const{value:$}=o;$&&(L===void 0?$.scrollTo(T):$.scrollTo(T,L))}else{const{value:$}=r;$&&$.scrollTo(T,L)}}re(eo,t);let s=0,v=0;const z=T=>{const L=T.target;s=L.scrollLeft,v=L.scrollTop,t.onScroll?.(T)};Xe(()=>{if(t.nativeScrollbar){const T=o.value;T&&(T.scrollTop=v,T.scrollLeft=s)}});const b={display:"flex",flexWrap:"nowrap",width:"100%",flexDirection:"row"},C={scrollTo:c},R=f(()=>{const{common:{cubicBezierEaseInOut:T},self:L}=d.value;return{"--n-bezier":T,"--n-color":t.embedded?L.colorEmbedded:L.color,"--n-text-color":L.textColor}}),N=l?ue("layout",f(()=>t.embedded?"e":""),R,t):void 0;return{mergedClsPrefix:a,scrollableElRef:o,scrollbarInstRef:r,hasSiderStyle:b,mergedTheme:d,handleNativeElScroll:z,cssVars:l?void 0:R,themeClass:N?.themeClass,onRender:N?.onRender,...C}},render(){const{mergedClsPrefix:t,hasSider:o}=this;this.onRender?.();const r=o?this.hasSiderStyle:void 0,a=[this.themeClass,e&&`${t}-layout-content`,`${t}-layout`,`${t}-layout--${this.position}-positioned`];return u(),w("div",{class:_(a),style:Y(this.cssVars)},[this.nativeScrollbar?(u(),w("div",{key:0,ref:"scrollableElRef",class:_([`${t}-layout-scroll-container`,this.contentClass]),style:Y([this.contentStyle,r]),onScroll:this.handleNativeElScroll},[A(()=>this.$slots.default?.())],46,["onScroll"])):(u(),E(Ye,Q({key:1},this.scrollbarProps,{onScroll:this.onScroll,ref:"scrollbarInstRef",theme:this.mergedTheme.peers.Scrollbar,themeOverrides:this.mergedTheme.peerOverrides.Scrollbar,contentClass:this.contentClass,contentStyle:[this.contentStyle,r]}),qe(this.$slots),1040,["onScroll","theme","themeOverrides","contentClass","contentStyle"]))],6)}})}var je=oo(!1),at=oo(!0),st=m("layout-header",`
 transition:
 color .3s var(--n-bezier),
 background-color .3s var(--n-bezier),
 box-shadow .3s var(--n-bezier),
 border-color .3s var(--n-bezier);
 box-sizing: border-box;
 width: 100%;
 background-color: var(--n-color);
 color: var(--n-text-color);
`,[k("absolute-positioned",`
 position: absolute;
 left: 0;
 right: 0;
 top: 0;
 `),k("bordered",`
 border-bottom: solid 1px var(--n-border-color);
 `)]);const ct={position:Le,inverted:Boolean,bordered:Boolean};var dt=j({name:"LayoutHeader",props:{...Z.props,...ct},setup(e){const{mergedClsPrefixRef:t,inlineThemeDisabled:o}=de(e),r=Z("Layout","-layout-header",st,Ee,e,t),a=f(()=>{const{common:{cubicBezierEaseInOut:d},self:c}=r.value,s={"--n-bezier":d};return e.inverted?(s["--n-color"]=c.headerColorInverted,s["--n-text-color"]=c.textColorInverted,s["--n-border-color"]=c.headerBorderColorInverted):(s["--n-color"]=c.headerColor,s["--n-text-color"]=c.textColor,s["--n-border-color"]=c.headerBorderColor),s}),l=o?ue("layout-header",f(()=>e.inverted?"a":"b"),a,e):void 0;return{mergedClsPrefix:t,cssVars:o?void 0:a,themeClass:l?.themeClass,onRender:l?.onRender}},render(){const{mergedClsPrefix:e}=this;return this.onRender?.(),u(),w("div",{class:_([`${e}-layout-header`,this.themeClass,this.position&&`${e}-layout-header--${this.position}-positioned`,this.bordered&&`${e}-layout-header--bordered`]),style:Y(this.cssVars)},[A(()=>this.$slots.default?.())],6)}}),ut=m("layout-sider",`
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
`,[k("bordered",[h("border",`
 content: "";
 position: absolute;
 top: 0;
 bottom: 0;
 width: 1px;
 background-color: var(--n-border-color);
 transition: background-color .3s var(--n-bezier);
 `)]),h("left-placement",[k("bordered",[h("border",`
 right: 0;
 `)])]),k("right-placement",`
 justify-content: flex-start;
 `,[k("bordered",[h("border",`
 left: 0;
 `)]),k("collapsed",[m("layout-toggle-button",[m("base-icon",`
 transform: rotate(180deg);
 `)]),m("layout-toggle-bar",[I("&:hover",[h("top",{transform:"rotate(-12deg) scale(1.15) translateY(-2px)"}),h("bottom",{transform:"rotate(12deg) scale(1.15) translateY(2px)"})])])]),m("layout-toggle-button",`
 left: 0;
 transform: translateX(-50%) translateY(-50%);
 `,[m("base-icon",`
 transform: rotate(0);
 `)]),m("layout-toggle-bar",`
 left: -28px;
 transform: rotate(180deg);
 `,[I("&:hover",[h("top",{transform:"rotate(12deg) scale(1.15) translateY(-2px)"}),h("bottom",{transform:"rotate(-12deg) scale(1.15) translateY(2px)"})])])]),k("collapsed",[m("layout-toggle-bar",[I("&:hover",[h("top",{transform:"rotate(-12deg) scale(1.15) translateY(-2px)"}),h("bottom",{transform:"rotate(12deg) scale(1.15) translateY(2px)"})])]),m("layout-toggle-button",[m("base-icon",`
 transform: rotate(0);
 `)])]),m("layout-toggle-button",`
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
 `,[m("base-icon",`
 transition: transform .3s var(--n-bezier);
 transform: rotate(180deg);
 `)]),m("layout-toggle-bar",`
 cursor: pointer;
 height: 72px;
 width: 32px;
 position: absolute;
 top: calc(50% - 36px);
 right: -28px;
 `,[h("top, bottom",`
 position: absolute;
 width: 4px;
 border-radius: 2px;
 height: 38px;
 left: 14px;
 transition: 
 background-color .3s var(--n-bezier),
 transform .3s var(--n-bezier);
 `),h("bottom",`
 position: absolute;
 top: 34px;
 `),I("&:hover",[h("top",{transform:"rotate(12deg) scale(1.15) translateY(-2px)"}),h("bottom",{transform:"rotate(-12deg) scale(1.15) translateY(2px)"})]),h("top, bottom",{backgroundColor:"var(--n-toggle-bar-color)"}),I("&:hover",[h("top, bottom",{backgroundColor:"var(--n-toggle-bar-color-hover)"})])]),h("border",`
 position: absolute;
 top: 0;
 right: 0;
 bottom: 0;
 width: 1px;
 transition: background-color .3s var(--n-bezier);
 `),m("layout-sider-scroll-container",`
 flex-grow: 1;
 flex-shrink: 0;
 box-sizing: border-box;
 height: 100%;
 opacity: 0;
 transition: opacity .3s var(--n-bezier);
 max-width: 100%;
 `),k("show-content",[m("layout-sider-scroll-container",{opacity:1})]),k("absolute-positioned",`
 position: absolute;
 left: 0;
 top: 0;
 bottom: 0;
 `)]);const vt=["onClick"];var ht=j({props:{clsPrefix:{type:String,required:!0},onClick:Function},render(){const{clsPrefix:e}=this;return u(),w("div",{onClick:this.onClick,class:_(`${e}-layout-toggle-bar`)},[ee("div",{class:_(`${e}-layout-toggle-bar__top`)},null,2),ee("div",{class:_(`${e}-layout-toggle-bar__bottom`)},null,2)],10,vt)}});const mt=["onClick"];var ft=j({name:"LayoutToggleButton",props:{clsPrefix:{type:String,required:!0},onClick:Function},render(){const{clsPrefix:e}=this;return u(),w("div",{class:_(`${e}-layout-toggle-button`),onClick:this.onClick},[(u(),E(Ze,{clsPrefix:e},{default:()=>(u(),E(Mo))},1032,["clsPrefix"]))],10,mt)}});const pt=["onTransitionend"],gt={position:Le,bordered:Boolean,collapsedWidth:{type:Number,default:48},width:{type:[Number,String],default:272},contentClass:String,contentStyle:{type:[String,Object],default:""},collapseMode:{type:String,default:"transform"},collapsed:{type:Boolean,default:void 0},defaultCollapsed:Boolean,showCollapsedContent:{type:Boolean,default:!0},showTrigger:{type:[Boolean,String],default:!1},nativeScrollbar:{type:Boolean,default:!0},inverted:Boolean,scrollbarProps:Object,triggerClass:String,triggerStyle:[String,Object],collapsedTriggerClass:String,collapsedTriggerStyle:[String,Object],"onUpdate:collapsed":[Function,Array],onUpdateCollapsed:[Function,Array],onAfterEnter:Function,onAfterLeave:Function,onExpand:[Function,Array],onCollapse:[Function,Array],onScroll:Function};var bt=j({name:"LayoutSider",props:{...Z.props,...gt},setup(e){const t=X(eo),o=M(null),r=M(null),a=M(e.defaultCollapsed),l=He(ge(e,"collapsed"),a),d=f(()=>ze(l.value?e.collapsedWidth:e.width)),c=f(()=>e.collapseMode!=="transform"?{}:{minWidth:ze(e.width)}),s=f(()=>t?t.siderPlacement:"left");function v(S,x){if(e.nativeScrollbar){const{value:F}=o;F&&(x===void 0?F.scrollTo(S):F.scrollTo(S,x))}else{const{value:F}=r;F&&F.scrollTo(S,x)}}function z(){const{"onUpdate:collapsed":S,onUpdateCollapsed:x,onExpand:F,onCollapse:D}=e,{value:U}=l;x&&q(x,!U),S&&q(S,!U),a.value=!U,U?F&&q(F):D&&q(D)}let b=0,C=0;const R=S=>{const x=S.target;b=x.scrollLeft,C=x.scrollTop,e.onScroll?.(S)};Xe(()=>{if(e.nativeScrollbar){const S=o.value;S&&(S.scrollTop=C,S.scrollLeft=b)}}),re(Qe,{collapsedRef:l,collapseModeRef:ge(e,"collapseMode")});const{mergedClsPrefixRef:N,inlineThemeDisabled:T}=de(e),L=Z("Layout","-layout-sider",ut,Ee,e,N);function $(S){S.propertyName==="max-width"&&(l.value?e.onAfterLeave?.():e.onAfterEnter?.())}const g={scrollTo:v},H=f(()=>{const{common:{cubicBezierEaseInOut:S},self:x}=L.value,{siderToggleButtonColor:F,siderToggleButtonBorder:D,siderToggleBarColor:U,siderToggleBarColorHover:le}=x,K={"--n-bezier":S,"--n-toggle-button-color":F,"--n-toggle-button-border":D,"--n-toggle-bar-color":U,"--n-toggle-bar-color-hover":le};return e.inverted?(K["--n-color"]=x.siderColorInverted,K["--n-text-color"]=x.textColorInverted,K["--n-border-color"]=x.siderBorderColorInverted,K["--n-toggle-button-icon-color"]=x.siderToggleButtonIconColorInverted,K.__invertScrollbar=x.__invertScrollbar):(K["--n-color"]=x.siderColor,K["--n-text-color"]=x.textColor,K["--n-border-color"]=x.siderBorderColor,K["--n-toggle-button-icon-color"]=x.siderToggleButtonIconColor),K}),O=T?ue("layout-sider",f(()=>e.inverted?"a":"b"),H,e):void 0;return{scrollableElRef:o,scrollbarInstRef:r,mergedClsPrefix:N,mergedTheme:L,styleMaxWidth:d,mergedCollapsed:l,scrollContainerStyle:c,siderPlacement:s,handleNativeElScroll:R,handleTransitionend:$,handleTriggerClick:z,inlineThemeDisabled:T,cssVars:H,themeClass:O?.themeClass,onRender:O?.onRender,...g}},render(){const{mergedClsPrefix:e,mergedCollapsed:t,showTrigger:o}=this;return this.onRender?.(),u(),w("aside",{class:_([`${e}-layout-sider`,this.themeClass,`${e}-layout-sider--${this.position}-positioned`,`${e}-layout-sider--${this.siderPlacement}-placement`,this.bordered&&`${e}-layout-sider--bordered`,t&&`${e}-layout-sider--collapsed`,(!t||this.showCollapsedContent)&&`${e}-layout-sider--show-content`]),onTransitionend:this.handleTransitionend,style:Y([this.inlineThemeDisabled?void 0:this.cssVars,{maxWidth:this.styleMaxWidth,width:ze(this.width)}])},[this.nativeScrollbar?(u(),w("div",{key:1,class:_([`${e}-layout-sider-scroll-container`,this.contentClass]),onScroll:this.handleNativeElScroll,style:Y([this.scrollContainerStyle,{overflow:"auto"},this.contentStyle]),ref:"scrollableElRef"},[A(()=>this.$slots.default?.())],46,["onScroll"])):(u(),E(Ye,Q({key:0},this.scrollbarProps,{onScroll:this.onScroll,ref:"scrollbarInstRef",style:this.scrollContainerStyle,contentStyle:this.contentStyle,contentClass:this.contentClass,theme:this.mergedTheme.peers.Scrollbar,themeOverrides:this.mergedTheme.peerOverrides.Scrollbar,builtinThemeOverrides:this.inverted&&this.cssVars.__invertScrollbar==="true"?{colorHover:"rgba(255, 255, 255, .4)",color:"rgba(255, 255, 255, .3)"}:void 0}),qe(this.$slots),1040,["onScroll","style","contentStyle","contentClass","theme","themeOverrides","builtinThemeOverrides"])),o?(u(),w(J,{key:2},[o==="bar"?(u(),E(ht,{key:0,clsPrefix:e,class:_(t?this.collapsedTriggerClass:this.triggerClass),style:Y(t?this.collapsedTriggerStyle:this.triggerStyle),onClick:this.handleTriggerClick},null,8,["clsPrefix","class","style","onClick"])):(u(),E(ft,{key:1,clsPrefix:e,class:_(t?this.collapsedTriggerClass:this.triggerClass),style:Y(t?this.collapsedTriggerStyle:this.triggerStyle),onClick:this.handleTriggerClick},null,8,["clsPrefix","class","style","onClick"]))],64)):A(()=>null),this.bordered?(u(),w("div",{key:4,class:_(`${e}-layout-sider__border`)},null,2)):A(()=>null)],46,pt)}});const ve=ne("n-menu"),to=ne("n-submenu"),Be=ne("n-menu-item-group"),De=[I("&::before","background-color: var(--n-item-color-hover);"),h("arrow",`
 color: var(--n-arrow-color-hover);
 `),h("icon",`
 color: var(--n-item-icon-color-hover);
 `),m("menu-item-content-header",`
 color: var(--n-item-text-color-hover);
 `,[I("a",`
 color: var(--n-item-text-color-hover);
 `),h("extra",`
 color: var(--n-item-text-color-hover);
 `)])],Ue=[h("icon",`
 color: var(--n-item-icon-color-hover-horizontal);
 `),m("menu-item-content-header",`
 color: var(--n-item-text-color-hover-horizontal);
 `,[I("a",`
 color: var(--n-item-text-color-hover-horizontal);
 `),h("extra",`
 color: var(--n-item-text-color-hover-horizontal);
 `)])];var Ct=I([m("menu",`
 background-color: var(--n-color);
 color: var(--n-item-text-color);
 overflow: hidden;
 transition: background-color .3s var(--n-bezier);
 box-sizing: border-box;
 font-size: var(--n-font-size);
 padding-bottom: 6px;
 `,[k("horizontal",`
 max-width: 100%;
 width: 100%;
 display: flex;
 overflow: hidden;
 padding-bottom: 0;
 `,[m("submenu","margin: 0;"),m("menu-item","margin: 0;"),m("menu-item-content",`
 padding: 0 20px;
 border-bottom: 2px solid #0000;
 `,[I("&::before","display: none;"),k("selected","border-bottom: 2px solid var(--n-border-color-horizontal)")]),m("menu-item-content",[k("selected",[h("icon","color: var(--n-item-icon-color-active-horizontal);"),m("menu-item-content-header",`
 color: var(--n-item-text-color-active-horizontal);
 `,[I("a","color: var(--n-item-text-color-active-horizontal);"),h("extra","color: var(--n-item-text-color-active-horizontal);")])]),k("child-active",`
 border-bottom: 2px solid var(--n-border-color-horizontal);
 `,[m("menu-item-content-header",`
 color: var(--n-item-text-color-child-active-horizontal);
 `,[I("a",`
 color: var(--n-item-text-color-child-active-horizontal);
 `),h("extra",`
 color: var(--n-item-text-color-child-active-horizontal);
 `)]),h("icon",`
 color: var(--n-item-icon-color-child-active-horizontal);
 `)]),ae("disabled",[ae("selected, child-active",[I("&:focus-within",Ue)]),k("selected",[oe(null,[h("icon","color: var(--n-item-icon-color-active-hover-horizontal);"),m("menu-item-content-header",`
 color: var(--n-item-text-color-active-hover-horizontal);
 `,[I("a","color: var(--n-item-text-color-active-hover-horizontal);"),h("extra","color: var(--n-item-text-color-active-hover-horizontal);")])])]),k("child-active",[oe(null,[h("icon","color: var(--n-item-icon-color-child-active-hover-horizontal);"),m("menu-item-content-header",`
 color: var(--n-item-text-color-child-active-hover-horizontal);
 `,[I("a","color: var(--n-item-text-color-child-active-hover-horizontal);"),h("extra","color: var(--n-item-text-color-child-active-hover-horizontal);")])])]),oe("border-bottom: 2px solid var(--n-border-color-horizontal);",Ue)]),m("menu-item-content-header",[I("a","color: var(--n-item-text-color-horizontal);")])])]),ae("responsive",[m("menu-item-content-header",`
 overflow: hidden;
 text-overflow: ellipsis;
 `)]),k("collapsed",[m("menu-item-content",[k("selected",[I("&::before",`
 background-color: var(--n-item-color-active-collapsed) !important;
 `)]),m("menu-item-content-header","opacity: 0;"),h("arrow","opacity: 0;"),h("icon","color: var(--n-item-icon-color-collapsed);")])]),m("menu-item",`
 height: var(--n-item-height);
 margin-top: 6px;
 position: relative;
 `),m("menu-item-content",`
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
 `,[I("> *","z-index: 1;"),I("&::before",`
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
 `),k("disabled",`
 opacity: .45;
 cursor: not-allowed;
 `),k("collapsed",[h("arrow","transform: rotate(0);")]),k("selected",[I("&::before","background-color: var(--n-item-color-active);"),h("arrow","color: var(--n-arrow-color-active);"),h("icon","color: var(--n-item-icon-color-active);"),m("menu-item-content-header",`
 color: var(--n-item-text-color-active);
 `,[I("a","color: var(--n-item-text-color-active);"),h("extra","color: var(--n-item-text-color-active);")])]),k("child-active",[m("menu-item-content-header",`
 color: var(--n-item-text-color-child-active);
 `,[I("a",`
 color: var(--n-item-text-color-child-active);
 `),h("extra",`
 color: var(--n-item-text-color-child-active);
 `)]),h("arrow",`
 color: var(--n-arrow-color-child-active);
 `),h("icon",`
 color: var(--n-item-icon-color-child-active);
 `)]),ae("disabled",[ae("selected, child-active",[I("&:focus-within",De)]),k("selected",[oe(null,[h("arrow","color: var(--n-arrow-color-active-hover);"),h("icon","color: var(--n-item-icon-color-active-hover);"),m("menu-item-content-header",`
 color: var(--n-item-text-color-active-hover);
 `,[I("a","color: var(--n-item-text-color-active-hover);"),h("extra","color: var(--n-item-text-color-active-hover);")])])]),k("child-active",[oe(null,[h("arrow","color: var(--n-arrow-color-child-active-hover);"),h("icon","color: var(--n-item-icon-color-child-active-hover);"),m("menu-item-content-header",`
 color: var(--n-item-text-color-child-active-hover);
 `,[I("a","color: var(--n-item-text-color-child-active-hover);"),h("extra","color: var(--n-item-text-color-child-active-hover);")])])]),k("selected",[oe(null,[I("&::before","background-color: var(--n-item-color-active-hover);")])]),oe(null,De)]),h("icon",`
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
 `),h("arrow",`
 grid-area: arrow;
 font-size: 16px;
 color: var(--n-arrow-color);
 transform: rotate(180deg);
 opacity: 1;
 transition:
 color .3s var(--n-bezier),
 transform 0.2s var(--n-bezier),
 opacity 0.2s var(--n-bezier);
 `),m("menu-item-content-header",`
 grid-area: content;
 transition:
 color .3s var(--n-bezier),
 opacity .3s var(--n-bezier);
 opacity: 1;
 white-space: nowrap;
 color: var(--n-item-text-color);
 `,[I("a",`
 outline: none;
 text-decoration: none;
 transition: color .3s var(--n-bezier);
 color: var(--n-item-text-color);
 `,[I("&::before",`
 content: "";
 position: absolute;
 left: 0;
 right: 0;
 top: 0;
 bottom: 0;
 `)]),h("extra",`
 font-size: .93em;
 color: var(--n-group-text-color);
 transition: color .3s var(--n-bezier);
 `)])]),m("submenu",`
 cursor: pointer;
 position: relative;
 margin-top: 6px;
 `,[m("menu-item-content",`
 height: var(--n-item-height);
 `),m("submenu-children",`
 overflow: hidden;
 padding: 0;
 `,[Ho({duration:".2s"})])]),m("menu-item-group",[m("menu-item-group-title",`
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
 `)])]),m("menu-tooltip",[I("a",`
 color: inherit;
 text-decoration: none;
 `)]),m("menu-divider",`
 transition: background-color .3s var(--n-bezier);
 background-color: var(--n-divider-color);
 height: 1px;
 margin: 6px 18px;
 `)]);function oe(e,t){return[k("hover",e,t),I("&:hover",e,t)]}var xt=j({name:"MenuDivider",setup(){const{mergedClsPrefixRef:e,isHorizontalRef:t}=X(ve);return()=>t.value?null:(u(),w("div",{key:1,class:_(`${e.value}-menu-divider`)},null,2))}}),yt=j({name:"ChevronDownFilled",render(){return(()=>{const e=_e("f3af82a2aab086a5");return e[0]||(e[0]=ee("svg",{viewBox:"0 0 16 16",fill:"none",xmlns:"http://www.w3.org/2000/svg"},[ee("path",{d:"M3.20041 5.73966C3.48226 5.43613 3.95681 5.41856 4.26034 5.70041L8 9.22652L11.7397 5.70041C12.0432 5.41856 12.5177 5.43613 12.7996 5.73966C13.0815 6.0432 13.0639 6.51775 12.7603 6.7996L8.51034 10.7996C8.22258 11.0668 7.77743 11.0668 7.48967 10.7996L3.23966 6.7996C2.93613 6.51775 2.91856 6.0432 3.20041 5.73966Z",fill:"currentColor"})],-1))})()}});const zt=["onClick"];var ro=j({name:"MenuOptionContent",props:{collapsed:Boolean,disabled:Boolean,title:[String,Function],icon:Function,extra:[String,Function],showArrow:Boolean,childActive:Boolean,hover:Boolean,paddingLeft:Number,selected:Boolean,maxIconSize:{type:Number,required:!0},activeIconSize:{type:Number,required:!0},iconMarginRight:{type:Number,required:!0},clsPrefix:{type:String,required:!0},onClick:Function,tmNode:{type:Object,required:!0},isEllipsisPlaceholder:Boolean},setup(e){const{props:t}=X(ve);return{menuProps:t,style:f(()=>{const{paddingLeft:o}=e;return{paddingLeft:o&&`${o}px`}}),iconStyle:f(()=>{const{maxIconSize:o,activeIconSize:r,iconMarginRight:a}=e;return{width:`${o}px`,height:`${o}px`,fontSize:`${r}px`,marginRight:`${a}px`}})}},render(){const{clsPrefix:e,tmNode:t,menuProps:{renderIcon:o,renderLabel:r,renderExtra:a,expandIcon:l}}=this,d=o?o(t.rawNode):te(this.icon);return(()=>{const c=_e("7bb10afc6caf8fa4");return u(),w("div",{onClick:s=>{this.onClick?.(s)},role:"none",class:_([`${e}-menu-item-content`,{[`${e}-menu-item-content--selected`]:this.selected,[`${e}-menu-item-content--collapsed`]:this.collapsed,[`${e}-menu-item-content--child-active`]:this.childActive,[`${e}-menu-item-content--disabled`]:this.disabled,[`${e}-menu-item-content--hover`]:this.hover}]),style:Y(this.style)},[A(()=>d&&(u(),w("div",{class:_(`${e}-menu-item-content__icon`),style:Y(this.iconStyle),role:"none"},[A(()=>[d])],6))),ee("div",{class:_(`${e}-menu-item-content-header`),role:"none"},[this.isEllipsisPlaceholder?(u(),w(J,{key:0},[A(()=>this.title)],64)):(u(),w(J,{key:1},[r?(u(),w(J,{key:0},[A(()=>r(t.rawNode))],64)):(u(),w(J,{key:1},[A(()=>te(this.title))],64))],64)),this.extra||a?(u(),w("span",{key:2,class:_(`${e}-menu-item-content-header__extra`)},[c[0]||(c[0]=A(" ",-1)),a?(u(),w(J,{key:0},[A(()=>a(t.rawNode))],64)):(u(),w(J,{key:1},[A(()=>te(this.extra))],64))],2)):A(()=>null)],2),this.showArrow?(u(),E(Ze,{key:0,ariaHidden:!0,class:_(`${e}-menu-item-content__arrow`),clsPrefix:e},{default:()=>l?l(t.rawNode):(u(),E(yt,{key:1}))},1032,["class","clsPrefix"])):A(()=>null)],14,zt)})()}});const pe=8;function Fe(e){const t=X(ve),{props:o,mergedCollapsedRef:r}=t,a=X(to,null),l=X(Be,null),d=f(()=>o.mode==="horizontal"),c=f(()=>d.value?o.dropdownPlacement:"tmNodes"in e?"right-start":"right"),s=f(()=>Math.max(o.collapsedIconSize??o.iconSize,o.iconSize));return{dropdownPlacement:c,activeIconSize:f(()=>!d.value&&e.root&&r.value?o.collapsedIconSize??o.iconSize:o.iconSize),maxIconSize:s,paddingLeft:f(()=>{if(d.value)return;const{collapsedWidth:v,indent:z,rootIndent:b}=o,{root:C,isGroup:R}=e,N=b===void 0?z:b;return C?r.value?v/2-s.value/2:N:l&&typeof l.paddingLeftRef.value=="number"?r.value?v/2-s.value/2:z/2+l.paddingLeftRef.value:a&&typeof a.paddingLeftRef.value=="number"?(R?z/2:z)+a.paddingLeftRef.value:0}),iconMarginRight:f(()=>{const{collapsedWidth:v,indent:z,rootIndent:b}=o,{value:C}=s,{root:R}=e;return d.value||!R||!r.value?pe:(b===void 0?z:b)+C+pe-(v+C)/2}),NMenu:t,NSubmenu:a,NMenuOptionGroup:l}}const $e={internalKey:{type:[String,Number],required:!0},root:Boolean,isGroup:Boolean,level:{type:Number,required:!0},title:[String,Function],extra:[String,Function]},no={...$e,tmNode:{type:Object,required:!0},disabled:Boolean,icon:Function,onClick:Function},It=Ne(no),St=j({name:"MenuOption",props:no,setup(e){const t=Fe(e),{NSubmenu:o,NMenu:r,NMenuOptionGroup:a}=t,{props:l,mergedClsPrefixRef:d,mergedCollapsedRef:c}=r,s=o?o.mergedDisabledRef:a?a.mergedDisabledRef:{value:!1},v=f(()=>s.value||e.disabled);function z(C){const{onClick:R}=e;R&&R(C)}function b(C){v.value||(r.doSelect(e.internalKey,e.tmNode.rawNode),z(C))}return{mergedClsPrefix:d,dropdownPlacement:t.dropdownPlacement,paddingLeft:t.paddingLeft,iconMarginRight:t.iconMarginRight,maxIconSize:t.maxIconSize,activeIconSize:t.activeIconSize,mergedTheme:r.mergedThemeRef,menuProps:l,dropdownEnabled:Re(()=>e.root&&c.value&&l.mode!=="horizontal"&&!v.value),selected:Re(()=>r.mergedValueRef.value===e.internalKey),mergedDisabled:v,handleClick:b}},render(){const{mergedClsPrefix:e,mergedTheme:t,tmNode:o,menuProps:{renderLabel:r,nodeProps:a}}=this,l=a?.(o.rawNode);return u(),w("div",Q(l,{role:"menuitem",class:[`${e}-menu-item`,l?.class]}),[(u(),E(Ko,{theme:t.peers.Tooltip,themeOverrides:t.peerOverrides.Tooltip,trigger:"hover",placement:this.dropdownPlacement,disabled:!this.dropdownEnabled||this.title===void 0,internalExtraClass:["menu-tooltip"]},{default:()=>r?r(o.rawNode):te(this.title),trigger:()=>(u(),E(ro,{tmNode:o,clsPrefix:e,paddingLeft:this.paddingLeft,iconMarginRight:this.iconMarginRight,maxIconSize:this.maxIconSize,activeIconSize:this.activeIconSize,selected:this.selected,title:this.title,extra:this.extra,disabled:this.mergedDisabled,icon:this.icon,onClick:this.handleClick},null,8,["tmNode","clsPrefix","paddingLeft","iconMarginRight","maxIconSize","activeIconSize","selected","title","extra","disabled","icon","onClick"]))},1032,["theme","themeOverrides","placement","disabled"]))],16)}}),lo={...$e,tmNode:{type:Object,required:!0},tmNodes:{type:Array,required:!0}},wt=Ne(lo),At=j({name:"MenuOptionGroup",props:lo,setup(e){const t=Fe(e),{NSubmenu:o}=t,r=f(()=>o?.mergedDisabledRef.value?!0:e.tmNode.disabled);re(Be,{paddingLeftRef:t.paddingLeft,mergedDisabledRef:r});const{mergedClsPrefixRef:a,props:l}=X(ve);return function(){const{value:d}=a,c=t.paddingLeft.value,{nodeProps:s}=l,v=s?.(e.tmNode.rawNode);return(()=>{const z=_e("45eca6a63be5028b");return u(),w("div",{class:_(`${d}-menu-item-group`),role:"group"},[ee("div",Q(v,{class:[`${d}-menu-item-group-title`,v?.class],style:[v?.style||"",c!==void 0?`padding-left: ${c}px;`:""]}),[A(()=>te(e.title)),e.extra?(u(),w(J,{key:0},[z[0]||(z[0]=A(" ",-1)),A(()=>te(e.extra))],64)):A(()=>null)],16),ee("div",null,[A(()=>e.tmNodes.map(b=>Oe(b,l)))])],2)})()}}}),Rt=["aria-expanded","id"],Ht=["aria-expanded","id"],io={...$e,rawNodes:{type:Array,default:()=>[]},tmNodes:{type:Array,default:()=>[]},tmNode:{type:Object,required:!0},disabled:Boolean,icon:Function,onClick:Function,domId:String,virtualChildActive:{type:Boolean,default:void 0},isEllipsisPlaceholder:Boolean},Pt=Ne(io),Pe=j({name:"Submenu",props:io,setup(e){const t=Fe(e),{NMenu:o,NSubmenu:r}=t,{props:a,mergedCollapsedRef:l,mergedThemeRef:d}=o,c=f(()=>{const{disabled:C}=e;return r?.mergedDisabledRef.value||a.disabled?!0:C}),s=M(!1);re(to,{paddingLeftRef:t.paddingLeft,mergedDisabledRef:c}),re(Be,null);function v(){const{onClick:C}=e;C&&C()}function z(){c.value||(l.value||o.toggleExpand(e.internalKey),v())}function b(C){s.value=C}return{menuProps:a,mergedTheme:d,doSelect:o.doSelect,inverted:o.invertedRef,isHorizontal:o.isHorizontalRef,mergedClsPrefix:o.mergedClsPrefixRef,maxIconSize:t.maxIconSize,activeIconSize:t.activeIconSize,iconMarginRight:t.iconMarginRight,dropdownPlacement:t.dropdownPlacement,dropdownShow:s,paddingLeft:t.paddingLeft,mergedDisabled:c,mergedValue:o.mergedValueRef,childActive:Re(()=>e.virtualChildActive??o.activePathRef.value.includes(e.internalKey)),collapsed:f(()=>a.mode==="horizontal"?!1:l.value?!0:!o.mergedExpandedKeysRef.value.includes(e.internalKey)),dropdownEnabled:f(()=>!c.value&&(a.mode==="horizontal"||l.value)),handlePopoverShowChange:b,handleClick:z}},render(){const{mergedClsPrefix:e,menuProps:{renderIcon:t,renderLabel:o}}=this,r=()=>{const{isHorizontal:l,paddingLeft:d,collapsed:c,mergedDisabled:s,maxIconSize:v,activeIconSize:z,title:b,childActive:C,icon:R,handleClick:N,menuProps:{nodeProps:T},dropdownShow:L,iconMarginRight:$,tmNode:g,mergedClsPrefix:H,isEllipsisPlaceholder:O,extra:S}=this,x=T?.(g.rawNode);return u(),w("div",Q(x,{class:[`${H}-menu-item`,x?.class],role:"menuitem"}),[(u(),E(ro,{tmNode:g,paddingLeft:d,collapsed:c,disabled:s,iconMarginRight:$,maxIconSize:v,activeIconSize:z,title:b,extra:S,showArrow:!l,childActive:C,clsPrefix:H,icon:R,hover:L,onClick:N,isEllipsisPlaceholder:O},null,8,["tmNode","paddingLeft","collapsed","disabled","iconMarginRight","maxIconSize","activeIconSize","title","extra","showArrow","childActive","clsPrefix","icon","hover","onClick","isEllipsisPlaceholder"]))],16)},a=()=>(u(),E(Po,null,{default:()=>{const{tmNodes:l,collapsed:d}=this;return d?null:(u(),w("div",{key:1,class:_(`${e}-submenu-children`),role:"menu"},[A(()=>l.map(c=>Oe(c,this.menuProps)))],2))}},1024));return this.root?(u(),E(Je,Q({key:2,size:"large",trigger:"hover"},this.menuProps?.dropdownProps,{themeOverrides:this.mergedTheme.peerOverrides.Dropdown,theme:this.mergedTheme.peers.Dropdown,builtinThemeOverrides:{fontSizeLarge:"14px",optionIconSizeLarge:"18px"},value:this.mergedValue,disabled:!this.dropdownEnabled,placement:this.dropdownPlacement,keyField:this.menuProps.keyField,labelField:this.menuProps.labelField,childrenField:this.menuProps.childrenField,onUpdateShow:this.handlePopoverShowChange,options:this.rawNodes,onSelect:this.doSelect,inverted:this.inverted,renderIcon:t,renderLabel:o}),{default:()=>(u(),w("div",{class:_(`${e}-submenu`),role:"menu","aria-expanded":!this.collapsed,id:this.domId},[A(()=>r()),this.isHorizontal?A(()=>null):(u(),w(J,{key:1},[A(()=>a())],64))],10,Rt))},1040,["themeOverrides","theme","value","disabled","placement","keyField","labelField","childrenField","onUpdateShow","options","onSelect","inverted","renderIcon","renderLabel"])):(u(),w("div",{key:3,class:_(`${e}-submenu`),role:"menu","aria-expanded":!this.collapsed,id:this.domId},[A(()=>r()),A(()=>a())],10,Ht))}});function ke(e){return e.type==="divider"||e.type==="render"}function kt(e){return e.type==="divider"}function Oe(e,t){const{rawNode:o}=e,{show:r}=o;if(r===!1)return null;if(ke(o))return kt(o)?(u(),E(xt,Q({key:e.key},o.props),null,16)):null;const{labelField:a}=t,{key:l,level:d,isGroup:c}=e,s={...o,title:o.title||o[a],extra:o.titleExtra||o.extra,key:l,internalKey:l,level:d,root:d===0,isGroup:c};return e.children?e.isGroup?ce(At,Ce(s,wt,{tmNode:e,tmNodes:e.children,key:l})):ce(Pe,Ce(s,Pt,{key:l,rawNodes:o[t.childrenField],tmNodes:e.children,tmNode:e})):ce(St,Ce(s,It,{key:l,tmNode:e}))}const Tt={...Z.props,options:{type:Array,default:()=>[]},collapsed:{type:Boolean,default:void 0},collapsedWidth:{type:Number,default:48},iconSize:{type:Number,default:20},collapsedIconSize:{type:Number,default:24},rootIndent:Number,indent:{type:Number,default:32},labelField:{type:String,default:"label"},keyField:{type:String,default:"key"},childrenField:{type:String,default:"children"},disabledField:{type:String,default:"disabled"},defaultExpandAll:Boolean,defaultExpandedKeys:Array,expandedKeys:Array,value:[String,Number],defaultValue:{type:[String,Number],default:null},mode:{type:String,default:"vertical"},watchProps:{type:Array,default:void 0},disabled:Boolean,show:{type:Boolean,default:!0},inverted:Boolean,"onUpdate:expandedKeys":[Function,Array],onUpdateExpandedKeys:[Function,Array],onUpdateValue:[Function,Array],"onUpdate:value":[Function,Array],expandIcon:Function,renderIcon:Function,renderLabel:Function,renderExtra:Function,dropdownProps:Object,accordion:Boolean,nodeProps:Function,dropdownPlacement:{type:String,default:"bottom"},responsive:Boolean,items:Array,onOpenNamesChange:[Function,Array],onSelect:[Function,Array],onExpandedNamesChange:[Function,Array],expandedNames:Array,defaultExpandedNames:Array};var _t=j({name:"Menu",inheritAttrs:!1,props:Tt,setup(e){const{mergedClsPrefixRef:t,inlineThemeDisabled:o}=de(e),r=Z("Menu","-menu",Ct,rt,e,t),a=X(Qe,null),l=f(()=>{const{collapsed:p}=e;if(p!==void 0)return p;if(a){const{collapseModeRef:P,collapsedRef:n}=a;if(P.value==="width")return n.value??!1}return!1}),d=f(()=>{const{keyField:p,childrenField:P,disabledField:n}=e;return ye(e.items||e.options,{getIgnored(y){return ke(y)},getChildren(y){return y[P]},getDisabled(y){return y[n]},getKey(y){return y[p]??y.name}})}),c=f(()=>new Set(d.value.treeNodes.map(p=>p.key))),{watchProps:s}=e,v=M(null);s?.includes("defaultValue")?Ae(()=>{v.value=e.defaultValue}):v.value=e.defaultValue;const z=ge(e,"value"),b=He(z,v),C=M([]),R=()=>{C.value=e.defaultExpandAll?d.value.getNonLeafKeys():e.defaultExpandedNames||e.defaultExpandedKeys||d.value.getPath(b.value,{includeSelf:!1}).keyPath};s?.includes("defaultExpandedKeys")?Ae(R):R();const N=Do(e,["expandedNames","expandedKeys"]),T=He(N,C),L=f(()=>d.value.treeNodes),$=f(()=>d.value.getPath(b.value).keyPath);re(ve,{props:e,mergedCollapsedRef:l,mergedThemeRef:r,mergedValueRef:b,mergedExpandedKeysRef:T,activePathRef:$,mergedClsPrefixRef:t,isHorizontalRef:f(()=>e.mode==="horizontal"),invertedRef:ge(e,"inverted"),doSelect:g,toggleExpand:O});function g(p,P){const{"onUpdate:value":n,onUpdateValue:y,onSelect:G}=e;y&&q(y,p,P),n&&q(n,p,P),G&&q(G,p,P),v.value=p}function H(p){const{"onUpdate:expandedKeys":P,onUpdateExpandedKeys:n,onExpandedNamesChange:y,onOpenNamesChange:G}=e;P&&q(P,p),n&&q(n,p),y&&q(y,p),G&&q(G,p),C.value=p}function O(p){const P=Array.from(T.value),n=P.findIndex(y=>y===p);if(~n)P.splice(n,1);else{if(e.accordion&&c.value.has(p)){const y=P.findIndex(G=>c.value.has(G));y>-1&&P.splice(y,1)}P.push(p)}H(P)}const S=p=>{const P=d.value.getPath(p??b.value,{includeSelf:!1}).keyPath;if(!P.length)return;const n=Array.from(T.value),y=new Set([...n,...P]);e.accordion&&c.value.forEach(G=>{y.has(G)&&!P.includes(G)&&y.delete(G)}),H(Array.from(y))},x=f(()=>{const{inverted:p}=e,{common:{cubicBezierEaseInOut:P},self:n}=r.value,{borderRadius:y,borderColorHorizontal:G,fontSize:mo,itemHeight:fo,dividerColor:po}=n,i={"--n-divider-color":po,"--n-bezier":P,"--n-font-size":mo,"--n-border-color-horizontal":G,"--n-border-radius":y,"--n-item-height":fo};return p?(i["--n-group-text-color"]=n.groupTextColorInverted,i["--n-color"]=n.colorInverted,i["--n-item-text-color"]=n.itemTextColorInverted,i["--n-item-text-color-hover"]=n.itemTextColorHoverInverted,i["--n-item-text-color-active"]=n.itemTextColorActiveInverted,i["--n-item-text-color-child-active"]=n.itemTextColorChildActiveInverted,i["--n-item-text-color-child-active-hover"]=n.itemTextColorChildActiveInverted,i["--n-item-text-color-active-hover"]=n.itemTextColorActiveHoverInverted,i["--n-item-icon-color"]=n.itemIconColorInverted,i["--n-item-icon-color-hover"]=n.itemIconColorHoverInverted,i["--n-item-icon-color-active"]=n.itemIconColorActiveInverted,i["--n-item-icon-color-active-hover"]=n.itemIconColorActiveHoverInverted,i["--n-item-icon-color-child-active"]=n.itemIconColorChildActiveInverted,i["--n-item-icon-color-child-active-hover"]=n.itemIconColorChildActiveHoverInverted,i["--n-item-icon-color-collapsed"]=n.itemIconColorCollapsedInverted,i["--n-item-text-color-horizontal"]=n.itemTextColorHorizontalInverted,i["--n-item-text-color-hover-horizontal"]=n.itemTextColorHoverHorizontalInverted,i["--n-item-text-color-active-horizontal"]=n.itemTextColorActiveHorizontalInverted,i["--n-item-text-color-child-active-horizontal"]=n.itemTextColorChildActiveHorizontalInverted,i["--n-item-text-color-child-active-hover-horizontal"]=n.itemTextColorChildActiveHoverHorizontalInverted,i["--n-item-text-color-active-hover-horizontal"]=n.itemTextColorActiveHoverHorizontalInverted,i["--n-item-icon-color-horizontal"]=n.itemIconColorHorizontalInverted,i["--n-item-icon-color-hover-horizontal"]=n.itemIconColorHoverHorizontalInverted,i["--n-item-icon-color-active-horizontal"]=n.itemIconColorActiveHorizontalInverted,i["--n-item-icon-color-active-hover-horizontal"]=n.itemIconColorActiveHoverHorizontalInverted,i["--n-item-icon-color-child-active-horizontal"]=n.itemIconColorChildActiveHorizontalInverted,i["--n-item-icon-color-child-active-hover-horizontal"]=n.itemIconColorChildActiveHoverHorizontalInverted,i["--n-arrow-color"]=n.arrowColorInverted,i["--n-arrow-color-hover"]=n.arrowColorHoverInverted,i["--n-arrow-color-active"]=n.arrowColorActiveInverted,i["--n-arrow-color-active-hover"]=n.arrowColorActiveHoverInverted,i["--n-arrow-color-child-active"]=n.arrowColorChildActiveInverted,i["--n-arrow-color-child-active-hover"]=n.arrowColorChildActiveHoverInverted,i["--n-item-color-hover"]=n.itemColorHoverInverted,i["--n-item-color-active"]=n.itemColorActiveInverted,i["--n-item-color-active-hover"]=n.itemColorActiveHoverInverted,i["--n-item-color-active-collapsed"]=n.itemColorActiveCollapsedInverted):(i["--n-group-text-color"]=n.groupTextColor,i["--n-color"]=n.color,i["--n-item-text-color"]=n.itemTextColor,i["--n-item-text-color-hover"]=n.itemTextColorHover,i["--n-item-text-color-active"]=n.itemTextColorActive,i["--n-item-text-color-child-active"]=n.itemTextColorChildActive,i["--n-item-text-color-child-active-hover"]=n.itemTextColorChildActiveHover,i["--n-item-text-color-active-hover"]=n.itemTextColorActiveHover,i["--n-item-icon-color"]=n.itemIconColor,i["--n-item-icon-color-hover"]=n.itemIconColorHover,i["--n-item-icon-color-active"]=n.itemIconColorActive,i["--n-item-icon-color-active-hover"]=n.itemIconColorActiveHover,i["--n-item-icon-color-child-active"]=n.itemIconColorChildActive,i["--n-item-icon-color-child-active-hover"]=n.itemIconColorChildActiveHover,i["--n-item-icon-color-collapsed"]=n.itemIconColorCollapsed,i["--n-item-text-color-horizontal"]=n.itemTextColorHorizontal,i["--n-item-text-color-hover-horizontal"]=n.itemTextColorHoverHorizontal,i["--n-item-text-color-active-horizontal"]=n.itemTextColorActiveHorizontal,i["--n-item-text-color-child-active-horizontal"]=n.itemTextColorChildActiveHorizontal,i["--n-item-text-color-child-active-hover-horizontal"]=n.itemTextColorChildActiveHoverHorizontal,i["--n-item-text-color-active-hover-horizontal"]=n.itemTextColorActiveHoverHorizontal,i["--n-item-icon-color-horizontal"]=n.itemIconColorHorizontal,i["--n-item-icon-color-hover-horizontal"]=n.itemIconColorHoverHorizontal,i["--n-item-icon-color-active-horizontal"]=n.itemIconColorActiveHorizontal,i["--n-item-icon-color-active-hover-horizontal"]=n.itemIconColorActiveHoverHorizontal,i["--n-item-icon-color-child-active-horizontal"]=n.itemIconColorChildActiveHorizontal,i["--n-item-icon-color-child-active-hover-horizontal"]=n.itemIconColorChildActiveHoverHorizontal,i["--n-arrow-color"]=n.arrowColor,i["--n-arrow-color-hover"]=n.arrowColorHover,i["--n-arrow-color-active"]=n.arrowColorActive,i["--n-arrow-color-active-hover"]=n.arrowColorActiveHover,i["--n-arrow-color-child-active"]=n.arrowColorChildActive,i["--n-arrow-color-child-active-hover"]=n.arrowColorChildActiveHover,i["--n-item-color-hover"]=n.itemColorHover,i["--n-item-color-active"]=n.itemColorActive,i["--n-item-color-active-hover"]=n.itemColorActiveHover,i["--n-item-color-active-collapsed"]=n.itemColorActiveCollapsed),i}),F=o?ue("menu",f(()=>e.inverted?"a":"b"),x,e):void 0,D=ko(),U=M(null),le=M(null);let K=!0;const he=()=>{K?K=!1:U.value?.sync({showAllItemsBeforeCalculate:!0})};function ie(){return document.getElementById(D)}const me=M(-1);function ao(p){me.value=e.options.length-p}function so(p){p||(me.value=-1)}const co=f(()=>{const p=me.value;return{children:p===-1?[]:e.options.slice(p)}}),uo=f(()=>{const{childrenField:p,disabledField:P,keyField:n}=e;return ye([co.value],{getIgnored(y){return ke(y)},getChildren(y){return y[p]},getDisabled(y){return y[P]},getKey(y){return y[n]??y.name}})}),vo=f(()=>ye([{}]).treeNodes[0]);function ho(){if(me.value===-1)return u(),E(Pe,{root:!0,level:0,key:"__ellpisisGroupPlaceholder__",internalKey:"__ellpisisGroupPlaceholder__",title:"···",tmNode:vo.value,domId:D,isEllipsisPlaceholder:!0},null,8,["tmNode","domId"]);const p=uo.value.treeNodes[0],P=$.value,n=!!p.children?.some(y=>P.includes(y.key));return u(),E(Pe,{level:0,root:!0,key:"__ellpisisGroup__",internalKey:"__ellpisisGroup__",title:"···",virtualChildActive:n,tmNode:p,domId:D,rawNodes:p.rawNode.children||[],tmNodes:p.children||[],isEllipsisPlaceholder:!0},null,8,["virtualChildActive","tmNode","domId","rawNodes","tmNodes"])}return{mergedClsPrefix:t,controlledExpandedKeys:N,uncontrolledExpanededKeys:C,mergedExpandedKeys:T,uncontrolledValue:v,mergedValue:b,activePath:$,tmNodes:L,mergedTheme:r,mergedCollapsed:l,cssVars:o?void 0:x,themeClass:F?.themeClass,overflowRef:U,counterRef:le,updateCounter:()=>{},onResize:he,onUpdateOverflow:so,onUpdateCount:ao,renderCounter:ho,getCounter:ie,onRender:F?.onRender,showOption:S,deriveResponsiveState:he}},render(){const{mergedClsPrefix:e,mode:t,themeClass:o,onRender:r}=this;r?.();const a=()=>this.tmNodes.map(c=>Oe(c,this.$props)),l=t==="horizontal"&&this.responsive,d=()=>ce("div",Q(this.$attrs,{role:t==="horizontal"?"menubar":"menu",class:[`${e}-menu`,o,`${e}-menu--${t}`,l&&`${e}-menu--responsive`,this.mergedCollapsed&&`${e}-menu--collapsed`],style:this.cssVars}),l?(u(),E(Vo,{key:2,ref:"overflowRef",onUpdateOverflow:this.onUpdateOverflow,getCounter:this.getCounter,onUpdateCount:this.onUpdateCount,updateCounter:this.updateCounter,style:{width:"100%",display:"flex",overflow:"hidden"}},{default:a,counter:this.renderCounter},1032,["onUpdateOverflow","getCounter","onUpdateCount","updateCounter"])):a());return l?(u(),E(Ge,{key:3,onResize:this.onResize},{default:d},1032,["onResize"])):d()}});const Nt=To("app",{state:()=>({sidebarCollapsed:!1}),actions:{setSidebarCollapsed(e){this.sidebarCollapsed=e},toggleSidebar(){this.sidebarCollapsed=!this.sidebarCollapsed}}}),Et=j({__name:"AppLayout",setup(e){const t=Nt(),o=_o(),r=Bo(),a=Lo(),l=[{label:"Dashboard",key:"dashboard"},{label:"Servers",key:"servers"}],d=[{label:"Sign out",key:"sign-out"}],c=f(()=>String(r.name??"dashboard")),s=f(()=>r.meta.title??"Gotham"),v=f(()=>o.user?.email??""),z=f(()=>(o.user?.email?.[0]??"?").toUpperCase());function b(R){a.push({name:String(R)})}async function C(R){R==="sign-out"&&(await o.logout(),await a.push({name:"login"}))}return(R,N)=>(u(),E(B(je),{class:"shell","has-sider":""},{default:V(()=>[W(B(bt),{bordered:"","collapse-mode":"width",collapsed:B(t).sidebarCollapsed,"collapsed-width":64,width:240,"show-trigger":"","onUpdate:collapsed":B(t).setSidebarCollapsed},{default:V(()=>[N[0]||(N[0]=ee("div",{class:"brand"},"Gotham",-1)),W(B(_t),{value:c.value,options:l,collapsed:B(t).sidebarCollapsed,"collapsed-width":64,"collapsed-icon-size":20,"onUpdate:value":b},null,8,["value","collapsed"])]),_:1},8,["collapsed","onUpdate:collapsed"]),W(B(je),null,{default:V(()=>[W(B(dt),{class:"topbar",bordered:""},{default:V(()=>[W(B(Ke),{strong:""},{default:V(()=>[fe(xe(s.value),1)]),_:1}),W(B(Ve),{align:"center"},{default:V(()=>[B(o).isAuthenticated?(u(),E(B(Je),{key:0,trigger:"click",options:d,onSelect:C},{default:V(()=>[W(B(Me),{quaternary:""},{default:V(()=>[W(B(Ve),{align:"center",size:8},{default:V(()=>[W(B(et),{round:"",size:28,src:B(o).user?.avatar},{default:V(()=>[fe(xe(z.value),1)]),_:1},8,["src"]),W(B(Ke),{depth:"2"},{default:V(()=>[fe(xe(v.value),1)]),_:1})]),_:1})]),_:1})]),_:1})):(u(),E(B(No),{key:1,to:"/login"},{default:V(()=>[W(B(Me),{quaternary:"",type:"primary"},{default:V(()=>[...N[1]||(N[1]=[fe("Sign in",-1)])]),_:1})]),_:1}))]),_:1})]),_:1}),W(B(at),{class:"content","content-style":"padding: 24px;"},{default:V(()=>[W(B(Eo))]),_:1})]),_:1})]),_:1}))}}),Ot=jo(Et,[["__scopeId","data-v-14593436"]]);export{Ot as default};
