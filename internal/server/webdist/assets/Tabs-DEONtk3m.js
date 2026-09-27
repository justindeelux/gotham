import{bn as It,bo as he,bp as Ot,d as re,K as jt,bq as Ht,q as U,aE as Dt,a as K,H as C,I as r,aX as Ge,V as l,an as E,b9 as Mt,ba as Ft,J as ge,bl as ue,o as i,c as b,M as f,Z as G,S as v,O as Vt,N as qe,a5 as Ye,y as ee,br as Nt,P as Y,aP as Ut,ac as Ke,ay as Xt,L as ve,F as X,l as V,bg as Gt,ax as qt,aV as Yt,a3 as Je,B as Kt,T as Oe,bs as Se,ae as Jt,z as ie,g as Zt,af as Qt,a7 as er,bt as tr,a4 as je,aD as se,bu as fe,a8 as rr,bv as ar,bw as nr,Y as or,aw as Q,at as pe}from"./index-BQvpBbOo.js";import{g as ir,u as $e}from"./text-Dh9zJxOX.js";import{A as sr}from"./Add-BckaiBzj.js";import{C as lr}from"./ChevronRight-agkixpKK.js";import{u as dr}from"./use-merged-state-Cx0FWrJE.js";import{c as cr,d as He,o as br}from"./Popover-BhuLs8vc.js";var fr=/\s/;function pr(e){for(var n=e.length;n--&&fr.test(e.charAt(n)););return n}var ur=/^\s+/;function hr(e){return e&&e.slice(0,pr(e)+1).replace(ur,"")}var De=NaN,vr=/^[-+]0x[0-9a-f]+$/i,gr=/^0b[01]+$/i,mr=/^0o[0-7]+$/i,xr=parseInt;function Me(e){if(typeof e=="number")return e;if(It(e))return De;if(he(e)){var n=typeof e.valueOf=="function"?e.valueOf():e;e=he(n)?n+"":n}if(typeof e!="string")return e===0?e:+e;e=hr(e);var s=gr.test(e);return s||mr.test(e)?xr(e.slice(2),s?2:8):vr.test(e)?De:+e}var Re=function(){return Ot.Date.now()},yr="Expected a function",Cr=Math.max,wr=Math.min;function Sr(e,n,s){var c,d,S,w,p,g,y=0,W=!1,h=!1,B=!0;if(typeof e!="function")throw new TypeError(yr);n=Me(n)||0,he(s)&&(W=!!s.leading,h="maxWait"in s,S=h?Cr(Me(s.maxWait)||0,n):S,B="trailing"in s?!!s.trailing:B);function I(x){var z=c,M=d;return c=d=void 0,y=x,w=e.apply(M,z),w}function _(x){return y=x,p=setTimeout(k,n),W?I(x):w}function $(x){var z=x-g,M=x-y,L=n-z;return h?wr(L,S-M):L}function m(x){var z=x-g,M=x-y;return g===void 0||z>=n||z<0||h&&M>=S}function k(){var x=Re();if(m(x))return q(x);p=setTimeout(k,$(x))}function q(x){return p=void 0,B&&c?I(x):(c=d=void 0,w)}function j(){p!==void 0&&clearTimeout(p),y=0,c=g=d=p=void 0}function H(){return p===void 0?w:q(Re())}function D(){var x=Re(),z=m(x);if(c=arguments,d=this,g=x,z){if(p===void 0)return _(g);if(h)return clearTimeout(p),p=setTimeout(k,n),I(g)}return p===void 0&&(p=setTimeout(k,n)),w}return D.cancel=j,D.flush=H,D}var Rr="Expected a function";function zr(e,n,s){var c=!0,d=!0;if(typeof e!="function")throw new TypeError(Rr);return he(s)&&(c="leading"in s?!!s.leading:c,d="trailing"in s?!!s.trailing:d),Sr(e,n,{leading:c,maxWait:n,trailing:d})}const Tr=He(".v-x-scroll",{overflow:"auto",scrollbarWidth:"none"},[He("&::-webkit-scrollbar",{width:0,height:0})]),$r=re({name:"XScroll",props:{disabled:Boolean,onScroll:Function},setup(){const e=U(null);function n(d){!(d.currentTarget.offsetWidth<d.currentTarget.scrollWidth)||d.deltaY===0||(d.currentTarget.scrollLeft+=d.deltaY+d.deltaX,d.preventDefault())}const s=Ht();return Tr.mount({id:"vueuc/x-scroll",head:!0,anchorMetaName:cr,ssr:s}),Object.assign({selfRef:e,handleWheel:n},{scrollTo(...d){var S;(S=e.value)===null||S===void 0||S.scrollTo(...d)}})},render(){return jt("div",{ref:"selfRef",onScroll:this.onScroll,onWheel:this.disabled?void 0:this.handleWheel,class:"v-x-scroll"},this.$slots)}});var Pr=re({name:"ChevronLeft",render(){return(()=>{const e=Dt("dfe229c2639b2082");return e[0]||(e[0]=K("svg",{viewBox:"0 0 16 16",fill:"none",xmlns:"http://www.w3.org/2000/svg"},[K("path",{d:"M10.3536 3.14645C10.5488 3.34171 10.5488 3.65829 10.3536 3.85355L6.20711 8L10.3536 12.1464C10.5488 12.3417 10.5488 12.6583 10.3536 12.8536C10.1583 13.0488 9.84171 13.0488 9.64645 12.8536L5.14645 8.35355C4.95118 8.15829 4.95118 7.84171 5.14645 7.64645L9.64645 3.14645C9.84171 2.95118 10.1583 2.95118 10.3536 3.14645Z",fill:"currentColor"})],-1))})()}});function Fe(e,n="default",s=[]){const{children:c}=e;if(c!==null&&typeof c=="object"&&!Array.isArray(c)){const d=c[n];if(typeof d=="function")return d()}return s}var kr=C([r("descriptions",{fontSize:"var(--n-font-size)"},[r("descriptions-separator",`
 display: inline-block;
 margin: 0 8px 0 2px;
 `),r("descriptions-table-wrapper",[r("descriptions-table",[r("descriptions-table-row",[r("descriptions-table-header",{padding:"var(--n-th-padding)"}),r("descriptions-table-content",{padding:"var(--n-td-padding)"})])])]),Ge("bordered",[r("descriptions-table-wrapper",[r("descriptions-table",[r("descriptions-table-row",[C("&:last-child",[r("descriptions-table-content",{paddingBottom:0})])])])])]),l("left-label-placement",[r("descriptions-table-content",[C("> *",{verticalAlign:"top"})])]),l("left-label-align",[C("th",{textAlign:"left"})]),l("center-label-align",[C("th",{textAlign:"center"})]),l("right-label-align",[C("th",{textAlign:"right"})]),l("bordered",[r("descriptions-table-wrapper",`
 border-radius: var(--n-border-radius);
 overflow: hidden;
 background: var(--n-merged-td-color);
 border: 1px solid var(--n-merged-border-color);
 `,[r("descriptions-table",[r("descriptions-table-row",[C("&:not(:last-child)",[r("descriptions-table-content",{borderBottom:"1px solid var(--n-merged-border-color)"}),r("descriptions-table-header",{borderBottom:"1px solid var(--n-merged-border-color)"})]),r("descriptions-table-header",`
 font-weight: 400;
 background-clip: padding-box;
 background-color: var(--n-merged-th-color);
 `,[C("&:not(:last-child)",{borderRight:"1px solid var(--n-merged-border-color)"})]),r("descriptions-table-content",[C("&:not(:last-child)",{borderRight:"1px solid var(--n-merged-border-color)"})])])])])]),r("descriptions-header",`
 font-weight: var(--n-th-font-weight);
 font-size: 18px;
 transition: color .3s var(--n-bezier);
 line-height: var(--n-line-height);
 margin-bottom: 16px;
 color: var(--n-title-text-color);
 `),r("descriptions-table-wrapper",`
 transition:
 background-color .3s var(--n-bezier),
 border-color .3s var(--n-bezier);
 `,[r("descriptions-table",`
 width: 100%;
 border-collapse: separate;
 border-spacing: 0;
 box-sizing: border-box;
 `,[r("descriptions-table-row",`
 box-sizing: border-box;
 transition: border-color .3s var(--n-bezier);
 `,[r("descriptions-table-header",`
 font-weight: var(--n-th-font-weight);
 line-height: var(--n-line-height);
 display: table-cell;
 box-sizing: border-box;
 color: var(--n-th-text-color);
 transition:
 color .3s var(--n-bezier),
 background-color .3s var(--n-bezier),
 border-color .3s var(--n-bezier);
 `),r("descriptions-table-content",`
 vertical-align: top;
 line-height: var(--n-line-height);
 display: table-cell;
 box-sizing: border-box;
 color: var(--n-td-text-color);
 transition:
 color .3s var(--n-bezier),
 background-color .3s var(--n-bezier),
 border-color .3s var(--n-bezier);
 `,[E("content",`
 transition: color .3s var(--n-bezier);
 display: inline-block;
 color: var(--n-td-text-color);
 `)]),E("label",`
 font-weight: var(--n-th-font-weight);
 transition: color .3s var(--n-bezier);
 display: inline-block;
 margin-right: 14px;
 color: var(--n-th-text-color);
 `)])])])]),r("descriptions-table-wrapper",`
 --n-merged-th-color: var(--n-th-color);
 --n-merged-td-color: var(--n-td-color);
 --n-merged-border-color: var(--n-border-color);
 `),Mt(r("descriptions-table-wrapper",`
 --n-merged-th-color: var(--n-th-color-modal);
 --n-merged-td-color: var(--n-td-color-modal);
 --n-merged-border-color: var(--n-border-color-modal);
 `)),Ft(r("descriptions-table-wrapper",`
 --n-merged-th-color: var(--n-th-color-popover);
 --n-merged-td-color: var(--n-td-color-popover);
 --n-merged-border-color: var(--n-border-color-popover);
 `))]);const _r="DESCRIPTION_ITEM_FLAG";function Br(e){return typeof e=="object"&&e&&!Array.isArray(e)?e.type&&e.type.DESCRIPTION_ITEM_FLAG:!1}const Lr=["colspan"],Ar=["colspan"],Er=["colspan"],Wr=["colspan"],Ir={...ge.props,title:String,column:{type:Number,default:3},columns:Number,labelPlacement:{type:String,default:"top"},labelAlign:{type:String,default:"left"},separator:{type:String,default:":"},size:String,bordered:Boolean,labelClass:String,labelStyle:[Object,String],contentClass:String,contentStyle:[Object,String]};var qr=re({name:"Descriptions",props:Ir,slots:Object,setup(e){const{mergedClsPrefixRef:n,inlineThemeDisabled:s,mergedComponentPropsRef:c}=qe(e),d=ee(()=>e.size||c?.value?.Descriptions?.size||"medium"),S=ge("Descriptions","-descriptions",kr,Nt,e,n),w=ee(()=>{const{bordered:g}=e,y=d.value,{common:{cubicBezierEaseInOut:W},self:{titleTextColor:h,thColor:B,thColorModal:I,thColorPopover:_,thTextColor:$,thFontWeight:m,tdTextColor:k,tdColor:q,tdColorModal:j,tdColorPopover:H,borderColor:D,borderColorModal:x,borderColorPopover:z,borderRadius:M,lineHeight:L,[Y("fontSize",y)]:J,[Y(g?"thPaddingBordered":"thPadding",y)]:N,[Y(g?"tdPaddingBordered":"tdPadding",y)]:R}}=S.value;return{"--n-title-text-color":h,"--n-th-padding":N,"--n-td-padding":R,"--n-font-size":J,"--n-bezier":W,"--n-th-font-weight":m,"--n-line-height":L,"--n-th-text-color":$,"--n-td-text-color":k,"--n-th-color":B,"--n-th-color-modal":I,"--n-th-color-popover":_,"--n-td-color":q,"--n-td-color-modal":j,"--n-td-color-popover":H,"--n-border-radius":M,"--n-border-color":D,"--n-border-color-modal":x,"--n-border-color-popover":z}}),p=s?Ye("descriptions",ee(()=>{let g="";const{bordered:y}=e;return y&&(g+="a"),g+=d.value[0],g}),w,e):void 0;return{mergedClsPrefix:n,cssVars:s?void 0:w,themeClass:p?.themeClass,onRender:p?.onRender,compitableColumn:$e(e,["columns","column"]),inlineThemeDisabled:s,mergedSize:d}},render(){const e=this.$slots.default,n=e?ue(e()):[];n.length;const{contentClass:s,labelClass:c,compitableColumn:d,labelPlacement:S,labelAlign:w,mergedSize:p,bordered:g,title:y,cssVars:W,mergedClsPrefix:h,separator:B,onRender:I}=this;I?.();const _=n.filter(m=>Br(m)),$=_.reduce((m,k,q)=>{const j=k.props||{},H=_.length-1===q,D=["label"in j?j.label:Fe(k,"label")],x=[Fe(k)],z=j.span||1,M=m.span;m.span+=z;const L=j.labelStyle||j["label-style"]||this.labelStyle,J=j.contentStyle||j["content-style"]||this.contentStyle;if(S==="left")g?m.row.push((i(),b("th",{key:1,class:v([`${h}-descriptions-table-header`,c]),colspan:1,style:G(L)},[f(()=>D)],6)),(i(),b("td",{key:2,class:v([`${h}-descriptions-table-content`,s]),colspan:H?(d-M)*2+1:z*2-1,style:G(J)},[f(()=>x)],14,Lr))):m.row.push((i(),b("td",{key:3,class:v(`${h}-descriptions-table-content`),colspan:H?(d-M)*2:z*2},[K("span",{class:v([`${h}-descriptions-table-content__label`,c]),style:G(L)},[f(()=>[...D,B&&(i(),b("span",{key:4,class:v(`${h}-descriptions-separator`)},[f(()=>B)],2))])],6),K("span",{class:v([`${h}-descriptions-table-content__content`,s]),style:G(J)},[f(()=>x)],6)],10,Ar)));else{const N=H?(d-M)*2:z*2;m.row.push((i(),b("th",{key:5,class:v([`${h}-descriptions-table-header`,c]),colspan:N,style:G(L)},[f(()=>D)],14,Er))),m.secondRow.push((i(),b("td",{key:6,class:v([`${h}-descriptions-table-content`,s]),colspan:N,style:G(J)},[f(()=>x)],14,Wr)))}return(m.span>=d||H)&&(m.span=0,m.row.length&&(m.rows.push(m.row),m.row=[]),S!=="left"&&m.secondRow.length&&(m.rows.push(m.secondRow),m.secondRow=[])),m},{span:0,row:[],secondRow:[],rows:[]}).rows.map(m=>(i(),b("tr",{class:v(`${h}-descriptions-table-row`)},[f(()=>m)],2)));return i(),b("div",{style:G(W),class:v([`${h}-descriptions`,this.themeClass,`${h}-descriptions--${S}-label-placement`,`${h}-descriptions--${w}-label-align`,`${h}-descriptions--${p}-size`,g&&`${h}-descriptions--bordered`])},[y||this.$slots.header?(i(),b("div",{key:0,class:v(`${h}-descriptions-header`)},[f(()=>y||ir(this,"header"))],2)):f(()=>null),K("div",{class:v(`${h}-descriptions-table-wrapper`)},[K("table",{class:v(`${h}-descriptions-table`)},[K("tbody",null,[f(()=>S==="top"&&(i(),b("tr",{class:v(`${h}-descriptions-table-row`),style:{visibility:"collapse"}},[f(()=>Vt(d*2,(i(),b("td"))))],2))),f(()=>$)])],2)],2)],6)}});const Or={label:String,span:{type:Number,default:1},labelClass:String,labelStyle:[Object,String],contentClass:String,contentStyle:[Object,String]};var Yr=re({name:"DescriptionsItem",[_r]:!0,props:Or,slots:Object,render(){return null}});const ke=Ut("n-tabs"),Ze={tab:[String,Number,Object,Function],name:{type:[String,Number],required:!0},disabled:Boolean,displayDirective:{type:String,default:"if"},closable:{type:Boolean,default:void 0},tabProps:Object,label:[String,Number,Object,Function]};var Kr=re({__TAB_PANE__:!0,name:"TabPane",alias:["TabPanel"],props:Ze,slots:Object,setup(e){const n=Ke(ke,null);return n||Xt("tab-pane","`n-tab-pane` must be placed inside `n-tabs`."),{style:n.paneStyleRef,class:n.paneClassRef,mergedClsPrefix:n.mergedClsPrefixRef}},render(){return i(),b("div",{class:v([`${this.mergedClsPrefix}-tab-pane`,this.class]),style:G(this.style)},[f(()=>this.$slots.default?.())],6)}});const jr=["data-name","data-disabled"],Hr={internalLeftPadded:Boolean,internalAddable:Boolean,internalCreatedByPane:Boolean,...Yt(Ze,["displayDirective"])};var Pe=re({__TAB__:!0,inheritAttrs:!1,name:"Tab",props:Hr,setup(e){const{mergedClsPrefixRef:n,valueRef:s,typeRef:c,closableRef:d,tabStyleRef:S,addTabStyleRef:w,tabClassRef:p,addTabClassRef:g,tabChangeIdRef:y,onBeforeLeaveRef:W,triggerRef:h,handleAdd:B,activateTab:I,handleClose:_}=Ke(ke);return{trigger:h,mergedClosable:ee(()=>{if(e.internalAddable)return!1;const{closable:$}=e;return $===void 0?d.value:$}),style:S,addStyle:w,tabClass:p,addTabClass:g,clsPrefix:n,value:s,type:c,handleClose($){$.stopPropagation(),!e.disabled&&_(e.name)},activateTab(){if(e.disabled)return;if(e.internalAddable){B();return}const{name:$}=e,m=++y.id;if($!==s.value){const{value:k}=W;k?Promise.resolve(k(e.name,s.value)).then(q=>{q&&y.id===m&&I($)}):I($)}}}},render(){const{internalAddable:e,clsPrefix:n,name:s,disabled:c,label:d,tab:S,value:w,mergedClosable:p,trigger:g,$slots:{default:y}}=this,W=d??S;return i(),b("div",{class:v(`${n}-tabs-tab-wrapper`)},[this.internalLeftPadded?(i(),b("div",{key:0,class:v(`${n}-tabs-tab-pad`)},null,2)):f(()=>null),(i(),b("div",ve({key:s,"data-name":s,"data-disabled":c?!0:void 0},ve({class:[`${n}-tabs-tab`,w===s&&`${n}-tabs-tab--active`,c&&`${n}-tabs-tab--disabled`,p&&`${n}-tabs-tab--closable`,e&&`${n}-tabs-tab--addable`,e?this.addTabClass:this.tabClass],onClick:g==="click"?this.activateTab:void 0,onMouseenter:g==="hover"?this.activateTab:void 0,style:e?this.addStyle:this.style},this.internalCreatedByPane?this.tabProps||{}:this.$attrs)),[K("span",{class:v(`${n}-tabs-tab__label`)},[e?(i(),b(X,{key:0},[K("div",{class:v(`${n}-tabs-tab__height-placeholder`)}," ",2),(i(),V(Je,{clsPrefix:n},{default:()=>(i(),V(sr))},1032,["clsPrefix"]))],64)):(i(),b(X,{key:1},[y?(i(),b(X,{key:0},[f(()=>y())],64)):(i(),b(X,{key:1},[typeof W=="object"?(i(),b(X,{key:0},[f(()=>W)],64)):(i(),b(X,{key:1},[f(()=>Gt(W??s))],64))],64))],64))],2),p&&this.type==="card"?(i(),V(qt,{key:0,clsPrefix:n,class:v(`${n}-tabs-tab__close`),onClick:this.handleClose,disabled:c},null,8,["clsPrefix","class","onClick","disabled"])):f(()=>null)],16,jr))],2)}}),Dr=r("tabs",`
 box-sizing: border-box;
 width: 100%;
 display: flex;
 flex-direction: column;
 transition:
 background-color .3s var(--n-bezier),
 border-color .3s var(--n-bezier);
`,[C("&.transition-disabled",[r("tabs-tab",`
 transition: none !important;
 `),r("tabs-nav-scroll-content",`
 transition: none !important;
 `),r("tabs-tab-pad",`
 transition: none !important;
 `)]),l("segment-type",[r("tabs-rail",[C("&.transition-disabled",[r("tabs-capsule",`
 transition: none;
 `)])])]),l("top",[r("tab-pane",`
 padding: var(--n-pane-padding-top) var(--n-pane-padding-right) var(--n-pane-padding-bottom) var(--n-pane-padding-left);
 `)]),l("left",[r("tab-pane",`
 padding: var(--n-pane-padding-right) var(--n-pane-padding-bottom) var(--n-pane-padding-left) var(--n-pane-padding-top);
 `)]),l("left, right",`
 flex-direction: row;
 `,[r("tabs-bar",`
 width: 2px;
 right: 0;
 transition:
 top .2s var(--n-bezier),
 max-height .2s var(--n-bezier),
 background-color .3s var(--n-bezier);
 `),r("tabs-tab",`
 padding: var(--n-tab-padding-vertical); 
 `)]),l("right",`
 flex-direction: row-reverse;
 `,[r("tab-pane",`
 padding: var(--n-pane-padding-left) var(--n-pane-padding-top) var(--n-pane-padding-right) var(--n-pane-padding-bottom);
 `),r("tabs-bar",`
 left: 0;
 `)]),l("bottom",`
 flex-direction: column-reverse;
 justify-content: flex-end;
 `,[r("tab-pane",`
 padding: var(--n-pane-padding-bottom) var(--n-pane-padding-right) var(--n-pane-padding-top) var(--n-pane-padding-left);
 `),r("tabs-bar",`
 top: 0;
 `)]),r("tabs-rail",`
 position: relative;
 padding: 3px;
 border-radius: var(--n-tab-border-radius);
 width: 100%;
 background-color: var(--n-color-segment);
 transition: background-color .3s var(--n-bezier);
 display: flex;
 align-items: center;
 `,[r("tabs-capsule",`
 border-radius: var(--n-tab-border-radius);
 position: absolute;
 left: 0;
 top: 0;
 pointer-events: none;
 background-color: var(--n-tab-color-segment);
 box-shadow: 0 1px 3px 0 rgba(0, 0, 0, .08);
 transition: transform 0.3s var(--n-bezier);
 `),r("tabs-tab-wrapper",`
 flex-basis: 0;
 flex-grow: 1;
 display: flex;
 align-items: center;
 justify-content: center;
 `,[r("tabs-tab",`
 overflow: hidden;
 border-radius: var(--n-tab-border-radius);
 width: 100%;
 display: flex;
 align-items: center;
 justify-content: center;
 `,[l("active",`
 font-weight: var(--n-font-weight-strong);
 color: var(--n-tab-text-color-active);
 `),C("&:hover",`
 color: var(--n-tab-text-color-hover);
 `)])])]),l("flex",[r("tabs-nav",`
 width: 100%;
 position: relative;
 `,[r("tabs-wrapper",`
 width: 100%;
 `,[r("tabs-tab",`
 margin-right: 0;
 `)])])]),r("tabs-nav",`
 box-sizing: border-box;
 line-height: 1.5;
 display: flex;
 transition: border-color .3s var(--n-bezier);
 `,[E("prefix, suffix",`
 display: flex;
 align-items: center;
 `),E("prefix","padding-right: 16px;"),E("suffix","padding-left: 16px;")]),l("top, bottom",[C(">",[r("tabs-nav",[r("tabs-nav-scroll-wrapper",[C("&::before",`
 top: 0;
 bottom: 0;
 left: 0;
 width: 20px;
 `),C("&::after",`
 top: 0;
 bottom: 0;
 right: 0;
 width: 20px;
 `),l("shadow-start",[C("&::before",`
 box-shadow: inset 10px 0 8px -8px rgba(0, 0, 0, .12);
 `)]),l("shadow-end",[C("&::after",`
 box-shadow: inset -10px 0 8px -8px rgba(0, 0, 0, .12);
 `)])])])])]),l("left, right",[r("tabs-nav-scroll-content",`
 flex-direction: column;
 `),C(">",[r("tabs-nav",[r("tabs-nav-scroll-wrapper",[C("&::before",`
 top: 0;
 left: 0;
 right: 0;
 height: 20px;
 `),C("&::after",`
 bottom: 0;
 left: 0;
 right: 0;
 height: 20px;
 `),l("shadow-start",[C("&::before",`
 box-shadow: inset 0 10px 8px -8px rgba(0, 0, 0, .12);
 `)]),l("shadow-end",[C("&::after",`
 box-shadow: inset 0 -10px 8px -8px rgba(0, 0, 0, .12);
 `)])])])])]),r("tabs-nav-scroll-wrapper",`
 flex: 1;
 position: relative;
 overflow: hidden;
 `,[r("tabs-nav-y-scroll",`
 height: 100%;
 width: 100%;
 overflow-y: auto; 
 scrollbar-width: none;
 `,[C("&::-webkit-scrollbar, &::-webkit-scrollbar-track-piece, &::-webkit-scrollbar-thumb",`
 width: 0;
 height: 0;
 display: none;
 `)]),C("&::before, &::after",`
 transition: box-shadow .3s var(--n-bezier);
 pointer-events: none;
 content: "";
 position: absolute;
 z-index: 1;
 `),C("&.transition-disabled",[C("&::before, &::after",`
 transition: none;
 `)])]),r("tabs-nav-scroll-content",`
 display: flex;
 position: relative;
 min-width: 100%;
 min-height: 100%;
 width: fit-content;
 box-sizing: border-box;
 `),r("tabs-wrapper",`
 display: inline-flex;
 flex-wrap: nowrap;
 position: relative;
 `),r("tabs-tab-wrapper",`
 display: flex;
 flex-wrap: nowrap;
 flex-shrink: 0;
 flex-grow: 0;
 `),r("tabs-tab",`
 cursor: pointer;
 white-space: nowrap;
 flex-wrap: nowrap;
 display: inline-flex;
 align-items: center;
 color: var(--n-tab-text-color);
 font-size: var(--n-tab-font-size);
 background-clip: padding-box;
 padding: var(--n-tab-padding);
 transition:
 box-shadow .3s var(--n-bezier),
 color .3s var(--n-bezier),
 background-color .3s var(--n-bezier),
 border-color .3s var(--n-bezier);
 `,[l("disabled",{cursor:"not-allowed"}),E("close",`
 margin-inline-start: 6px;
 transition:
 background-color .3s var(--n-bezier),
 color .3s var(--n-bezier);
 `),E("label",`
 display: flex;
 align-items: center;
 z-index: 1;
 `)]),r("tabs-bar",`
 position: absolute;
 bottom: 0;
 height: 2px;
 border-radius: 1px;
 background-color: var(--n-bar-color);
 transition:
 left .2s var(--n-bezier),
 max-width .2s var(--n-bezier),
 opacity .3s var(--n-bezier),
 background-color .3s var(--n-bezier);
 `,[C("&.transition-disabled",`
 transition: none;
 `),l("disabled",`
 background-color: var(--n-tab-text-color-disabled)
 `)]),r("tabs-pane-wrapper",`
 position: relative;
 overflow: hidden;
 transition: max-height .2s var(--n-bezier);
 `),r("tab-pane",`
 color: var(--n-pane-text-color);
 width: 100%;
 transition:
 color .3s var(--n-bezier),
 background-color .3s var(--n-bezier),
 opacity .2s var(--n-bezier);
 left: 0;
 right: 0;
 top: 0;
 `,[C("&.next-transition-leave-active, &.prev-transition-leave-active, &.next-transition-enter-active, &.prev-transition-enter-active",`
 transition:
 color .3s var(--n-bezier),
 background-color .3s var(--n-bezier),
 transform .2s var(--n-bezier),
 opacity .2s var(--n-bezier);
 `),C("&.next-transition-leave-active, &.prev-transition-leave-active",`
 position: absolute;
 `),C("&.next-transition-enter-from, &.prev-transition-leave-to",`
 transform: translateX(32px);
 opacity: 0;
 `),C("&.next-transition-leave-to, &.prev-transition-enter-from",`
 transform: translateX(-32px);
 opacity: 0;
 `),C("&.next-transition-leave-from, &.next-transition-enter-to, &.prev-transition-leave-from, &.prev-transition-enter-to",`
 transform: translateX(0);
 opacity: 1;
 `)]),r("tabs-tab-pad",`
 box-sizing: border-box;
 width: var(--n-tab-gap);
 flex-grow: 0;
 flex-shrink: 0;
 `),l("line-type, bar-type",[r("tabs-tab",`
 font-weight: var(--n-tab-font-weight);
 box-sizing: border-box;
 vertical-align: bottom;
 `,[C("&:hover",{color:"var(--n-tab-text-color-hover)"}),l("active",`
 color: var(--n-tab-text-color-active);
 font-weight: var(--n-tab-font-weight-active);
 `),l("disabled",{color:"var(--n-tab-text-color-disabled)"})])]),r("tabs-nav",[E("prefix, suffix",`
 border-color: var(--n-tab-border-color);
 `),r("tabs-nav-scroll-content",`
 border-color: var(--n-tab-border-color);
 `),l("line-type",[l("top",[E("prefix, suffix",`
 border-bottom: 1px solid var(--n-tab-border-color);
 `),r("tabs-nav-scroll-content",`
 border-bottom: 1px solid var(--n-tab-border-color);
 `),r("tabs-bar",`
 bottom: -1px;
 `)]),l("left",[E("prefix, suffix",`
 border-right: 1px solid var(--n-tab-border-color);
 `),r("tabs-nav-scroll-content",`
 border-right: 1px solid var(--n-tab-border-color);
 `),r("tabs-bar",`
 right: -1px;
 `)]),l("right",[E("prefix, suffix",`
 border-left: 1px solid var(--n-tab-border-color);
 `),r("tabs-nav-scroll-content",`
 border-left: 1px solid var(--n-tab-border-color);
 `),r("tabs-bar",`
 left: -1px;
 `)]),l("bottom",[E("prefix, suffix",`
 border-top: 1px solid var(--n-tab-border-color);
 `),r("tabs-nav-scroll-content",`
 border-top: 1px solid var(--n-tab-border-color);
 `),r("tabs-bar",`
 top: -1px;
 `)]),E("prefix, suffix",`
 transition: border-color .3s var(--n-bezier);
 `),r("tabs-nav-scroll-content",`
 transition: border-color .3s var(--n-bezier);
 `),r("tabs-bar",`
 border-radius: 0;
 `)]),l("card-type",[E("prefix, suffix",`
 transition: border-color .3s var(--n-bezier);
 `),r("tabs-pad",`
 flex-grow: 1;
 transition: border-color .3s var(--n-bezier);
 `),r("tabs-tab-pad",`
 transition: border-color .3s var(--n-bezier);
 `),r("tabs-tab",`
 font-weight: var(--n-tab-font-weight);
 border: 1px solid var(--n-tab-border-color);
 background-color: var(--n-tab-color);
 box-sizing: border-box;
 position: relative;
 vertical-align: bottom;
 display: flex;
 justify-content: space-between;
 font-size: var(--n-tab-font-size);
 color: var(--n-tab-text-color);
 `,[l("addable",`
 padding-left: 8px;
 padding-right: 8px;
 font-size: 16px;
 justify-content: center;
 `,[E("height-placeholder",`
 width: 0;
 font-size: var(--n-tab-font-size);
 `),Ge("disabled",[C("&:hover",`
 color: var(--n-tab-text-color-hover);
 `)])]),l("closable","padding-inline-end: 8px;"),l("active",`
 background-color: #0000;
 font-weight: var(--n-tab-font-weight-active);
 color: var(--n-tab-text-color-active);
 `),l("disabled","color: var(--n-tab-text-color-disabled);")])]),l("left, right",`
 flex-direction: column; 
 `,[E("prefix, suffix",`
 padding: var(--n-tab-padding-vertical);
 `),r("tabs-wrapper",`
 flex-direction: column;
 `),r("tabs-tab-wrapper",`
 flex-direction: column;
 `,[r("tabs-tab-pad",`
 height: var(--n-tab-gap-vertical);
 width: 100%;
 `)])]),l("top",[l("card-type",[r("tabs-scroll-padding","border-bottom: 1px solid var(--n-tab-border-color);"),E("prefix, suffix",`
 border-bottom: 1px solid var(--n-tab-border-color);
 `),r("tabs-tab",`
 border-top-left-radius: var(--n-tab-border-radius);
 border-top-right-radius: var(--n-tab-border-radius);
 `,[l("active",`
 border-bottom: 1px solid #0000;
 `)]),r("tabs-tab-pad",`
 border-bottom: 1px solid var(--n-tab-border-color);
 `),r("tabs-pad",`
 border-bottom: 1px solid var(--n-tab-border-color);
 `)])]),l("left",[l("card-type",[r("tabs-scroll-padding","border-right: 1px solid var(--n-tab-border-color);"),E("prefix, suffix",`
 border-right: 1px solid var(--n-tab-border-color);
 `),r("tabs-tab",`
 border-top-left-radius: var(--n-tab-border-radius);
 border-bottom-left-radius: var(--n-tab-border-radius);
 `,[l("active",`
 border-right: 1px solid #0000;
 `)]),r("tabs-tab-pad",`
 border-right: 1px solid var(--n-tab-border-color);
 `),r("tabs-pad",`
 border-right: 1px solid var(--n-tab-border-color);
 `)])]),l("right",[l("card-type",[r("tabs-scroll-padding","border-left: 1px solid var(--n-tab-border-color);"),E("prefix, suffix",`
 border-left: 1px solid var(--n-tab-border-color);
 `),r("tabs-tab",`
 border-top-right-radius: var(--n-tab-border-radius);
 border-bottom-right-radius: var(--n-tab-border-radius);
 `,[l("active",`
 border-left: 1px solid #0000;
 `)]),r("tabs-tab-pad",`
 border-left: 1px solid var(--n-tab-border-color);
 `),r("tabs-pad",`
 border-left: 1px solid var(--n-tab-border-color);
 `)])]),l("bottom",[l("card-type",[r("tabs-scroll-padding","border-top: 1px solid var(--n-tab-border-color);"),E("prefix, suffix",`
 border-top: 1px solid var(--n-tab-border-color);
 `),r("tabs-tab",`
 border-bottom-left-radius: var(--n-tab-border-radius);
 border-bottom-right-radius: var(--n-tab-border-radius);
 `,[l("active",`
 border-top: 1px solid #0000;
 `)]),r("tabs-tab-pad",`
 border-top: 1px solid var(--n-tab-border-color);
 `),r("tabs-pad",`
 border-top: 1px solid var(--n-tab-border-color);
 `)])])]),r("tabs-scroll-button",[l("start",`
 padding-left: 10px;
 padding-right: 6px;
 `),l("end",`
 padding-right: 10px;
 padding-left: 6px;
 `),l("up",`
 padding-bottom: 10px;
 `),l("down",`
 padding-top: 10px;
 `)])]),Ve=re({name:"TabsButton",props:{type:{type:String,default:"next"},mergedClsPrefix:{type:String,required:!0},vertical:Boolean,disabled:Boolean,rtl:Boolean,theme:Object,themeOverrides:Object,onClick:Function},setup(e){return{handleClick:()=>{e.disabled||e.onClick?.(e.type)}}},render(){const{mergedClsPrefix:e,disabled:n,type:s,vertical:c,rtl:d,theme:S,themeOverrides:w,handleClick:p}=this,g=s==="next",y=c?g:d?!g:g;return i(),V(Kt,{text:!0,disabled:n,size:"small",theme:S,themeOverrides:w,onClick:p,class:v([`${e}-tabs-scroll-button`,!c&&s==="prev"&&`${e}-tabs-scroll-button--start`,!c&&s==="next"&&`${e}-tabs-scroll-button--end`,c&&s==="prev"&&`${e}-tabs-scroll-button--up`,c&&s==="next"&&`${e}-tabs-scroll-button--down`])},{icon:()=>(i(),V(Je,{clsPrefix:e,style:G(c?{transform:"rotate(90deg)"}:void 0)},{default:()=>y?(i(),V(lr,{key:1})):(i(),V(Pr,{key:2}))},1032,["clsPrefix","style"]))},1032,["disabled","theme","themeOverrides","onClick","class"])}});const ze=zr,Mr={...ge.props,value:[String,Number],defaultValue:[String,Number],trigger:{type:String,default:"click"},type:{type:String,default:"bar"},closable:Boolean,justifyContent:String,size:String,placement:{type:String,default:"top"},tabStyle:[String,Object],tabClass:String,addTabStyle:[String,Object],addTabClass:String,barWidth:Number,paneClass:String,paneStyle:[String,Object],paneWrapperClass:String,paneWrapperStyle:[String,Object],addable:[Boolean,Object],tabsPadding:{type:Number,default:0},animated:Boolean,onBeforeLeave:Function,onAdd:Function,"onUpdate:value":[Function,Array],onUpdateValue:[Function,Array],onClose:[Function,Array],labelSize:String,activeName:[String,Number],onActiveNameChange:[Function,Array],showScrollButton:Boolean,centerActiveTab:Boolean};var Jr=re({name:"Tabs",props:Mr,slots:Object,setup(e,{slots:n}){const{mergedClsPrefixRef:s,inlineThemeDisabled:c,mergedComponentPropsRef:d,mergedRtlRef:S}=qe(e),w=Jt("Tabs",S,s),p=ee(()=>{const{placement:t}=e;return t==="start"?w?.value?"right":"left":t==="end"?w?.value?"left":"right":t}),g=ge("Tabs","-tabs",Dr,tr,e,s),y=U(null),W=U(null),h=U(null),B=U(null),I=U(null),_=U(null),$=U(null),m=U(!0),k=U(!0),q=$e(e,["labelSize","size"]),j=ee(()=>{if(q.value)return q.value;const t=d?.value?.Tabs?.size;return t||"medium"}),H=$e(e,["activeName","value"]),D=U(H.value??e.defaultValue??(n.default?ue(n.default())[0]?.props?.name:null)),x=dr(H,D),z={id:0},M=ee(()=>{if(!(!e.justifyContent||e.type==="card"))return{display:"flex",justifyContent:e.justifyContent}});ie(x,()=>{z.id=0,F(),se(()=>{me()})});function L(){const{value:t}=x;return t===null?null:y.value?.querySelector(`[data-name="${t}"]`)}function J(t){if(e.type==="card")return;const{value:a}=h;if(!a)return;const o=a.style.opacity==="0";if(t){const u=`${s.value}-tabs-bar--disabled`,{barWidth:T}=e,A=p.value;if(t.dataset.disabled==="true"?a.classList.add(u):a.classList.remove(u),["top","bottom"].includes(A)){if(R(["top","maxHeight","height"]),typeof T=="number"&&t.offsetWidth>=T){const P=Math.floor((t.offsetWidth-T)/2)+t.offsetLeft;a.style.left=`${P}px`,a.style.maxWidth=`${T}px`}else a.style.left=`${t.offsetLeft}px`,a.style.maxWidth=`${t.offsetWidth}px`;a.style.width="8192px",o&&(a.style.transition="none"),a.offsetWidth,o&&(a.style.transition="",a.style.opacity="1")}else{if(R(["left","maxWidth","width"]),typeof T=="number"&&t.offsetHeight>=T){const P=Math.floor((t.offsetHeight-T)/2)+t.offsetTop;a.style.top=`${P}px`,a.style.maxHeight=`${T}px`}else a.style.top=`${t.offsetTop}px`,a.style.maxHeight=`${t.offsetHeight}px`;a.style.height="8192px",o&&(a.style.transition="none"),a.offsetHeight,o&&(a.style.transition="",a.style.opacity="1")}}}function N(){if(e.type==="card")return;const{value:t}=h;t&&(t.style.opacity="0")}function R(t){const{value:a}=h;if(a)for(const o of t)a.style[o]=""}function F(){if(e.type==="card")return;const t=L();t?J(t):N()}function ae(t,a,o,u){const T=t.getBoundingClientRect(),A=a.getBoundingClientRect(),P=o?"left":"top",O=o?"right":"bottom";let Z=0;u?Z=(A[P]+A[O])/2-(T[P]+T[O])/2:A[P]<T[P]?Z=A[P]-T[P]:A[O]>T[O]&&(Z=A[O]-T[O]),Z!==0&&t.scrollBy({[P]:Z,behavior:"smooth"})}function me(){const t=["top","bottom"].includes(p.value),a=L();if(a)if(t){const o=_.value?.$el;if(!o)return;ae(o,a,t,e.centerActiveTab)}else{const{value:o}=$;if(!o)return;ae(o,a,t,e.centerActiveTab)}}const le=U(null);let xe=0,te=null;function Qe(t){const a=le.value;if(a){xe=t.getBoundingClientRect().height;const o=`${xe}px`,u=()=>{a.style.height=o,a.style.maxHeight=o};te?(u(),te(),te=null):te=u}}function et(t){const a=le.value;if(a){const o=t.getBoundingClientRect().height,u=()=>{document.body.offsetHeight,a.style.maxHeight=`${o}px`,a.style.height=`${Math.max(xe,o)}px`};te?(te(),te=null,u()):te=u}}function tt(){const t=le.value;if(t){t.style.maxHeight="",t.style.height="";const{paneWrapperStyle:a}=e;if(typeof a=="string")t.style.cssText=a;else if(a){const{maxHeight:o,height:u}=a;o!==void 0&&(t.style.maxHeight=o),u!==void 0&&(t.style.height=u)}}}const _e={value:[]},Be=U("next");function rt(t){const a=x.value;let o="next";for(const u of _e.value){if(u===a)break;if(u===t){o="prev";break}}Be.value=o,at(t)}function at(t){const{onActiveNameChange:a,onUpdateValue:o,"onUpdate:value":u}=e;a&&pe(a,t),o&&pe(o,t),u&&pe(u,t),D.value=t}function nt(t){const{onClose:a}=e;a&&pe(a,t)}function ot(t){if(["top","bottom"].includes(p.value)){const{value:a}=_;if(!a)return;const o=a.$el;if(!o)return;const u=o.offsetWidth,T=!!w?.value,A=t==="next"?u:-u;o.scrollBy({left:T?-A:A,behavior:"smooth"})}else{const{value:a}=$;if(!a)return;const o=a.offsetHeight,u=t==="next"?a.scrollTop+o:a.scrollTop-o;a.scrollTo({top:u,left:0,behavior:"smooth"})}}let ye=!0;function Ce(){const{value:t}=h;if(!t)return;ye&&(ye=!1);const a="transition-disabled";t.classList.add(a),F(),t.classList.remove(a)}const ne=U(null);function de({transitionDisabled:t}){const a=y.value;if(!a)return;t&&a.classList.add("transition-disabled");const o=L();o&&ne.value&&(ne.value.style.width=`${o.offsetWidth}px`,ne.value.style.height=`${o.offsetHeight}px`,ne.value.style.transform=`translate(${o.offsetLeft}px, ${o.offsetTop}px)`,t&&ne.value.offsetWidth),t&&a.classList.remove("transition-disabled")}ie([x],()=>{e.type==="segment"&&se(()=>{de({transitionDisabled:!1})})}),Zt(()=>{e.type==="segment"&&de({transitionDisabled:!0})});let Le=0;function it(t){if(t.contentRect.width===0&&t.contentRect.height===0||Le===t.contentRect.width)return;Le=t.contentRect.width;const{type:a}=e;(a==="line"||a==="bar")&&(ye||e.justifyContent?.startsWith("space"))&&Ce(),a!=="segment"&&ce(Ee())}const st=ze(it,64);function Ae(){const{type:t}=e;t==="line"||t==="bar"?Ce():t==="segment"&&de({transitionDisabled:!0})}ie([()=>e.justifyContent,()=>e.size],()=>{se(()=>{(e.type==="line"||e.type==="bar")&&Ce()})}),ie([p,()=>w?.value],()=>{se(()=>{Ae(),ce(Ee(),{instantly:!0})})}),ie(()=>e.type,()=>{se(()=>{const t=W.value;t&&(t.classList.add("transition-disabled"),Ae(),t.offsetWidth,t.classList.remove("transition-disabled"))})});const oe=U(!1);function lt(t){const{target:a,contentRect:{width:o,height:u}}=t,T=a.parentElement.parentElement.offsetWidth,A=a.parentElement.parentElement.offsetHeight,P=p.value;if(!oe.value)P==="top"||P==="bottom"?T<o&&(oe.value=!0):A<u&&(oe.value=!0);else{const{value:O}=I;if(!O)return;P==="top"||P==="bottom"?T-o>O.$el.offsetWidth&&(oe.value=!1):A-u>O.$el.offsetHeight&&(oe.value=!1)}ce(_.value?.$el||null)}const dt=ze(lt,64);function ct(){const{onAdd:t}=e;t&&t()}const we=U(!1);function Ee(){const t=p.value;return(t==="top"||t==="bottom"?_.value?.$el:$.value)||null}function ce(t,a={instantly:!1}){if(!t)return;const o=a.instantly?B.value:null;o&&o.classList.add("transition-disabled");const u=1,T=p.value;if(T==="top"||T==="bottom"){const{scrollLeft:A,scrollWidth:P,offsetWidth:O}=t,Z=Math.abs(A);m.value=Z<=u,k.value=Z+O>=P-u,we.value=O<P-u}else{const{scrollTop:A,scrollHeight:P,offsetHeight:O}=t;m.value=A<=u,k.value=A+O>=P-u,we.value=O<P-u}o&&(o.offsetWidth,o.classList.remove("transition-disabled"))}const bt=ze(t=>{ce(t.target)},64);or(ke,{triggerRef:Q(e,"trigger"),tabStyleRef:Q(e,"tabStyle"),tabClassRef:Q(e,"tabClass"),addTabStyleRef:Q(e,"addTabStyle"),addTabClassRef:Q(e,"addTabClass"),paneClassRef:Q(e,"paneClass"),paneStyleRef:Q(e,"paneStyle"),mergedClsPrefixRef:s,typeRef:Q(e,"type"),closableRef:Q(e,"closable"),valueRef:x,tabChangeIdRef:z,onBeforeLeaveRef:Q(e,"onBeforeLeave"),activateTab:rt,handleClose:nt,handleAdd:ct}),br(()=>{F(),me()}),Qt(()=>{const{value:t}=B;if(!t)return;const{value:a}=s,o=`${a}-tabs-nav-scroll-wrapper--shadow-start`,u=`${a}-tabs-nav-scroll-wrapper--shadow-end`;m.value?t.classList.remove(o):t.classList.add(o),k.value?t.classList.remove(u):t.classList.add(u)});const ft={syncBarPosition:()=>{F()},scrollToCurrentTab:()=>{me()}},pt=()=>{de({transitionDisabled:!0})},We=ee(()=>{const{value:t}=j,{type:a}=e,o=`${t}${{card:"Card",bar:"Bar",line:"Line",segment:"Segment"}[a]}`,{self:{barColor:u,closeIconColor:T,closeIconColorHover:A,closeIconColorPressed:P,tabColor:O,tabBorderColor:Z,paneTextColor:ut,tabFontWeight:ht,tabBorderRadius:vt,tabFontWeightActive:gt,colorSegment:mt,fontWeightStrong:xt,tabColorSegment:yt,closeSize:Ct,closeIconSize:wt,closeColorHover:St,closeColorPressed:Rt,closeBorderRadius:zt,[Y("panePadding",t)]:be,[Y("tabPadding",o)]:Tt,[Y("tabPaddingVertical",o)]:$t,[Y("tabGap",o)]:Pt,[Y("tabGap",`${o}Vertical`)]:kt,[Y("tabTextColor",a)]:_t,[Y("tabTextColorActive",a)]:Bt,[Y("tabTextColorHover",a)]:Lt,[Y("tabTextColorDisabled",a)]:At,[Y("tabFontSize",t)]:Et},common:{cubicBezierEaseInOut:Wt}}=g.value;return{"--n-bezier":Wt,"--n-color-segment":mt,"--n-bar-color":u,"--n-tab-font-size":Et,"--n-tab-text-color":_t,"--n-tab-text-color-active":Bt,"--n-tab-text-color-disabled":At,"--n-tab-text-color-hover":Lt,"--n-pane-text-color":ut,"--n-tab-border-color":Z,"--n-tab-border-radius":vt,"--n-close-size":Ct,"--n-close-icon-size":wt,"--n-close-color-hover":St,"--n-close-color-pressed":Rt,"--n-close-border-radius":zt,"--n-close-icon-color":T,"--n-close-icon-color-hover":A,"--n-close-icon-color-pressed":P,"--n-tab-color":O,"--n-tab-font-weight":ht,"--n-tab-font-weight-active":gt,"--n-tab-padding":Tt,"--n-tab-padding-vertical":$t,"--n-tab-gap":Pt,"--n-tab-gap-vertical":kt,"--n-pane-padding-left":fe(be,"left"),"--n-pane-padding-right":fe(be,"right"),"--n-pane-padding-top":fe(be,"top"),"--n-pane-padding-bottom":fe(be,"bottom"),"--n-font-weight-strong":xt,"--n-tab-color-segment":yt}}),Ie=c?Ye("tabs",ee(()=>`${j.value[0]}${e.type[0]}`),We,e):void 0;return{mergedClsPrefix:s,mergedValue:x,renderedNames:new Set,segmentCapsuleElRef:ne,tabsPaneWrapperRef:le,tabsElRef:y,selfElRef:W,barElRef:h,addTabInstRef:I,xScrollInstRef:_,scrollWrapperElRef:B,addTabFixed:oe,tabWrapperStyle:M,handleNavResize:st,mergedSize:j,handleScroll:bt,handleTabsResize:dt,cssVars:c?void 0:We,themeClass:Ie?.themeClass,animationDirection:Be,renderNameListRef:_e,yScrollElRef:$,handleSegmentResize:pt,onAnimationBeforeLeave:Qe,onAnimationEnter:et,onAnimationAfterEnter:tt,onRender:Ie?.onRender,startReachedRef:m,endReachedRef:k,isOverflow:we,handleButtonClick:ot,mergedTheme:g,rtlEnabled:w,mergedPlacement:p,...ft}},render(){const{mergedClsPrefix:e,type:n,mergedPlacement:s,addTabFixed:c,addable:d,mergedSize:S,renderNameListRef:w,onRender:p,paneWrapperClass:g,paneWrapperStyle:y,startReachedRef:W,endReachedRef:h,isOverflow:B,showScrollButton:I,handleButtonClick:_,mergedTheme:$,rtlEnabled:m,$slots:{default:k,prefix:q,suffix:j}}=this;p?.();const H=k?ue(k()).filter(R=>R.type.__TAB_PANE__===!0):[],D=k?ue(k()).filter(R=>R.type.__TAB__===!0):[],x=!D.length,z=n==="card",M=n==="segment",L=!z&&!M&&this.justifyContent;w.value=[];const J=()=>{const R=(i(),b("div",{style:G(this.tabWrapperStyle),class:v(`${e}-tabs-wrapper`)},[L?f(()=>null):(i(),b("div",{key:1,class:v(`${e}-tabs-scroll-padding`),style:G(s==="top"||s==="bottom"?{width:`${this.tabsPadding}px`}:{height:`${this.tabsPadding}px`})},null,6)),x?(i(),b(X,{key:2},[f(()=>H.map((F,ae)=>(w.value.push(F.props.name),Te((i(),V(Pe,ve(F.props,{internalCreatedByPane:!0,internalLeftPadded:ae!==0&&(!L||L==="center"||L==="start"||L==="end")}),je(F.children?{default:F.children.tab}:void 0),1040,["internalLeftPadded"]))))))],64)):(i(),b(X,{key:3},[f(()=>D.map((F,ae)=>(w.value.push(F.props.name),Te(ae!==0&&!L?Xe(F):F))))],64)),!c&&d&&z?(i(),b(X,{key:4},[f(()=>Ue(d,(x?H.length:D.length)!==0))],64)):f(()=>null),L?f(()=>null):(i(),b("div",{key:7,class:v(`${e}-tabs-scroll-padding`),style:G({width:`${this.tabsPadding}px`})},null,6)),z?f(()=>null):(i(),b("div",{key:9,ref:"barElRef",class:v(`${e}-tabs-bar`)},null,2))],6));return i(),b("div",{ref:"tabsElRef",class:v(`${e}-tabs-nav-scroll-content`)},[z&&d?(i(),V(Se,{key:0,onResize:this.handleTabsResize},{default:()=>R},1032,["onResize"])):(i(),b(X,{key:1},[f(()=>R)],64)),z?(i(),b("div",{key:2,class:v(`${e}-tabs-pad`)},null,2)):f(()=>null)],2)},N=M?"top":s;return i(),b("div",{ref:"selfElRef",class:v([`${e}-tabs`,this.themeClass,`${e}-tabs--${n}-type`,`${e}-tabs--${S}-size`,L&&`${e}-tabs--flex`,`${e}-tabs--${N}`,m&&`${e}-tabs--rtl`]),style:G(this.cssVars)},[K("div",{class:v([`${e}-tabs-nav--${n}-type`,`${e}-tabs-nav--${N}`,`${e}-tabs-nav`])},[f(()=>Oe(q,R=>R&&(i(),b("div",{class:v(`${e}-tabs-nav__prefix`)},[f(()=>R)],2)))),M?(i(),V(Se,{key:0,onResize:this.handleSegmentResize},{default:()=>(i(),b("div",{class:v(`${e}-tabs-rail`),ref:"tabsElRef"},[K("div",{class:v(`${e}-tabs-capsule`),ref:"segmentCapsuleElRef"},[K("div",{class:v(`${e}-tabs-wrapper`)},[K("div",{class:v(`${e}-tabs-tab`)},null,2)],2)],2),x?(i(),b(X,{key:0},[f(()=>H.map((R,F)=>(w.value.push(R.props.name),i(),V(Pe,ve(R.props,{internalCreatedByPane:!0,internalLeftPadded:F!==0}),je(R.children?{default:R.children.tab}:void 0),1040,["internalLeftPadded"]))))],64)):(i(),b(X,{key:1},[f(()=>D.map((R,F)=>(w.value.push(R.props.name),F===0?R:Xe(R))))],64))],2))},1032,["onResize"])):(i(),b(X,{key:1},[f(()=>I&&B&&(i(),V(Ve,{mergedClsPrefix:e,type:"prev",vertical:N==="left"||N==="right",disabled:W,rtl:!!m,theme:$.peers.Button,themeOverrides:$.peerOverrides.Button,onClick:_},null,8,["mergedClsPrefix","vertical","disabled","rtl","theme","themeOverrides","onClick"]))),(i(),V(Se,{onResize:this.handleNavResize},{default:()=>(i(),b("div",{class:v(`${e}-tabs-nav-scroll-wrapper`),ref:"scrollWrapperElRef"},[["top","bottom"].includes(N)?(i(),V($r,{key:0,ref:"xScrollInstRef",onScroll:this.handleScroll},{default:J},1032,["onScroll"])):(i(),b("div",{key:1,class:v(`${e}-tabs-nav-y-scroll`),onScroll:this.handleScroll,ref:"yScrollElRef"},[f(()=>J())],42,["onScroll"]))],2))},1032,["onResize"])),f(()=>I&&B&&(i(),V(Ve,{mergedClsPrefix:e,type:"next",vertical:N==="left"||N==="right",disabled:h,rtl:!!m,theme:$.peers.Button,themeOverrides:$.peerOverrides.Button,onClick:_},null,8,["mergedClsPrefix","vertical","disabled","rtl","theme","themeOverrides","onClick"])))],64)),c&&d&&z?(i(),b(X,{key:2},[f(()=>Ue(d,!0))],64)):f(()=>null),f(()=>Oe(j,R=>R&&(i(),b("div",{class:v(`${e}-tabs-nav__suffix`)},[f(()=>R)],2))))],2),f(()=>x&&(this.animated&&(N==="top"||N==="bottom")?(i(),b("div",{key:1,ref:"tabsPaneWrapperRef",style:G(y),class:v([`${e}-tabs-pane-wrapper`,g])},[f(()=>Ne(H,this.mergedValue,this.renderedNames,this.onAnimationBeforeLeave,this.onAnimationEnter,this.onAnimationAfterEnter,this.animationDirection))],6)):Ne(H,this.mergedValue,this.renderedNames)))],6)}});function Ne(e,n,s,c,d,S,w){const p=[];return e.forEach(g=>{const{name:y,displayDirective:W,"display-directive":h}=g.props,B=_=>W===_||h===_,I=n===y;if(g.key!==void 0&&(g.key=y),I||B("show")||B("show:lazy")&&s.has(y)){s.has(y)||s.add(y);const _=!B("if");p.push(_?er(g,[[rr,I]]):g)}}),w?(i(),V(ar,{name:`${w}-transition`,onBeforeLeave:c,onEnter:d,onAfterEnter:S},{default:()=>p},1032,["name","onBeforeLeave","onEnter","onAfterEnter"])):p}function Ue(e,n){return i(),V(Pe,{ref:"addTabInstRef",key:"__addable",name:"__addable",internalCreatedByPane:!0,internalAddable:!0,internalLeftPadded:n,disabled:typeof e=="object"&&e.disabled},null,8,["internalLeftPadded","disabled"])}function Xe(e){const n=nr(e);return n.props?n.props.internalLeftPadded=!0:n.props={internalLeftPadded:!0},n}function Te(e){return Array.isArray(e.dynamicProps)?e.dynamicProps.includes("internalLeftPadded")||e.dynamicProps.push("internalLeftPadded"):e.dynamicProps=["internalLeftPadded"],e}export{qr as D,Jr as T,Kr as a,Yr as b};
