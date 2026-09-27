import{E as Ce}from"./Empty-DH4KFkum.js";import{A as ea}from"./Alert-DUH-sjSa.js";import{A as ta}from"./Avatar-CYb6eK2Z.js";import{av as aa,aw as $e,ax as ra,d as le,K as na,ay as oa,q as M,U as sa,a as L,H as k,I as a,az as ct,ac as p,ah as U,aA as la,aB as ia,J as Pe,aC as ke,o as i,c as x,M as C,ai as te,S as R,O as da,N as bt,al as ft,y as re,aD as ca,P as ae,ab as ba,a8 as ut,aE as fa,L as Te,F as ee,l as K,aF as ua,aG as pa,aH as va,a4 as pt,B as he,T as Ze,aI as We,_ as ha,z as ue,g as vt,aJ as ga,aK as ma,aL as xa,at as Qe,a0 as pe,aM as we,aN as ya,aO as Ca,aP as wa,af as _a,a6 as se,$ as _e,w as f,u as l,e as b,k as I,j as Oe,t as T,m as et,C as de,i as Sa,h as Ra}from"./index-OEEKQFJV.js";import{g as za,u as Ne,S as Se,t as Re}from"./text-DmSgZ5AK.js";import{u as ka}from"./use-message-DimMzRWd.js";import{A as $a,P as Ta}from"./Popconfirm-C9ePsUPF.js";import{S as Pa}from"./Spin-B07f1ahV.js";import{C as Ba}from"./ChevronRight-DmdtDRee.js";import{u as Aa}from"./use-merged-state-AkDci1sI.js";import{c as La,a as tt,o as Ea}from"./Popover-DfWJDHpE.js";import{g as Ia,d as De}from"./servers-CJTv7PO6.js";import{S as Wa,r as ze,t as Oa}from"./format-CrTtdsuE.js";import{u as Da}from"./useMediaQuery-CRpRkswt.js";import{u as ja}from"./servers-Ds3e-khp.js";import{_ as Ha}from"./_plugin-vue_export-helper-DlAUqK2U.js";import"./format-length-qbNAZ8nO.js";var Ma=/\s/;function Na(e){for(var o=e.length;o--&&Ma.test(e.charAt(o)););return o}var Fa=/^\s+/;function Va(e){return e&&e.slice(0,Na(e)+1).replace(Fa,"")}var at=NaN,Ua=/^[-+]0x[0-9a-f]+$/i,Ga=/^0b[01]+$/i,Xa=/^0o[0-7]+$/i,Ka=parseInt;function rt(e){if(typeof e=="number")return e;if(aa(e))return at;if($e(e)){var o=typeof e.valueOf=="function"?e.valueOf():e;e=$e(o)?o+"":o}if(typeof e!="string")return e===0?e:+e;e=Va(e);var c=Ga.test(e);return c||Xa.test(e)?Ka(e.slice(2),c?2:8):Ua.test(e)?at:+e}var je=function(){return ra.Date.now()},qa="Expected a function",Ya=Math.max,Ja=Math.min;function Za(e,o,c){var v,u,$,n,g,m,_=0,D=!1,y=!1,j=!0;if(typeof e!="function")throw new TypeError(qa);o=rt(o)||0,$e(c)&&(D=!!c.leading,y="maxWait"in c,$=y?Ya(rt(c.maxWait)||0,o):$,j="trailing"in c?!!c.trailing:j);function H(s){var z=v,Y=u;return v=u=void 0,_=s,n=e.apply(Y,z),n}function W(s){return _=s,g=setTimeout(P,o),D?H(s):n}function B(s){var z=s-m,Y=s-_,N=o-z;return y?Ja(N,$-Y):N}function h(s){var z=s-m,Y=s-_;return m===void 0||z>=o||z<0||y&&Y>=$}function P(){var s=je();if(h(s))return Z(s);g=setTimeout(P,B(s))}function Z(s){return g=void 0,j&&v?H(s):(v=u=void 0,n)}function G(){g!==void 0&&clearTimeout(g),_=0,v=m=u=g=void 0}function X(){return g===void 0?n:Z(je())}function S(){var s=je(),z=h(s);if(v=arguments,u=this,m=s,z){if(g===void 0)return W(m);if(y)return clearTimeout(g),g=setTimeout(P,o),H(m)}return g===void 0&&(g=setTimeout(P,o)),n}return S.cancel=G,S.flush=X,S}var Qa="Expected a function";function er(e,o,c){var v=!0,u=!0;if(typeof e!="function")throw new TypeError(Qa);return $e(c)&&(v="leading"in c?!!c.leading:v,u="trailing"in c?!!c.trailing:u),Za(e,o,{leading:v,maxWait:o,trailing:u})}const tr=tt(".v-x-scroll",{overflow:"auto",scrollbarWidth:"none"},[tt("&::-webkit-scrollbar",{width:0,height:0})]),ar=le({name:"XScroll",props:{disabled:Boolean,onScroll:Function},setup(){const e=M(null);function o(u){!(u.currentTarget.offsetWidth<u.currentTarget.scrollWidth)||u.deltaY===0||(u.currentTarget.scrollLeft+=u.deltaY+u.deltaX,u.preventDefault())}const c=oa();return tr.mount({id:"vueuc/x-scroll",head:!0,anchorMetaName:La,ssr:c}),Object.assign({selfRef:e,handleWheel:o},{scrollTo(...u){var $;($=e.value)===null||$===void 0||$.scrollTo(...u)}})},render(){return na("div",{ref:"selfRef",onScroll:this.onScroll,onWheel:this.disabled?void 0:this.handleWheel,class:"v-x-scroll"},this.$slots)}});var rr=le({name:"ChevronLeft",render(){return(()=>{const e=sa("dfe229c2639b2082");return e[0]||(e[0]=L("svg",{viewBox:"0 0 16 16",fill:"none",xmlns:"http://www.w3.org/2000/svg"},[L("path",{d:"M10.3536 3.14645C10.5488 3.34171 10.5488 3.65829 10.3536 3.85355L6.20711 8L10.3536 12.1464C10.5488 12.3417 10.5488 12.6583 10.3536 12.8536C10.1583 13.0488 9.84171 13.0488 9.64645 12.8536L5.14645 8.35355C4.95118 8.15829 4.95118 7.84171 5.14645 7.64645L9.64645 3.14645C9.84171 2.95118 10.1583 2.95118 10.3536 3.14645Z",fill:"currentColor"})],-1))})()}});function nt(e,o="default",c=[]){const{children:v}=e;if(v!==null&&typeof v=="object"&&!Array.isArray(v)){const u=v[o];if(typeof u=="function")return u()}return c}var nr=k([a("descriptions",{fontSize:"var(--n-font-size)"},[a("descriptions-separator",`
 display: inline-block;
 margin: 0 8px 0 2px;
 `),a("descriptions-table-wrapper",[a("descriptions-table",[a("descriptions-table-row",[a("descriptions-table-header",{padding:"var(--n-th-padding)"}),a("descriptions-table-content",{padding:"var(--n-td-padding)"})])])]),ct("bordered",[a("descriptions-table-wrapper",[a("descriptions-table",[a("descriptions-table-row",[k("&:last-child",[a("descriptions-table-content",{paddingBottom:0})])])])])]),p("left-label-placement",[a("descriptions-table-content",[k("> *",{verticalAlign:"top"})])]),p("left-label-align",[k("th",{textAlign:"left"})]),p("center-label-align",[k("th",{textAlign:"center"})]),p("right-label-align",[k("th",{textAlign:"right"})]),p("bordered",[a("descriptions-table-wrapper",`
 border-radius: var(--n-border-radius);
 overflow: hidden;
 background: var(--n-merged-td-color);
 border: 1px solid var(--n-merged-border-color);
 `,[a("descriptions-table",[a("descriptions-table-row",[k("&:not(:last-child)",[a("descriptions-table-content",{borderBottom:"1px solid var(--n-merged-border-color)"}),a("descriptions-table-header",{borderBottom:"1px solid var(--n-merged-border-color)"})]),a("descriptions-table-header",`
 font-weight: 400;
 background-clip: padding-box;
 background-color: var(--n-merged-th-color);
 `,[k("&:not(:last-child)",{borderRight:"1px solid var(--n-merged-border-color)"})]),a("descriptions-table-content",[k("&:not(:last-child)",{borderRight:"1px solid var(--n-merged-border-color)"})])])])])]),a("descriptions-header",`
 font-weight: var(--n-th-font-weight);
 font-size: 18px;
 transition: color .3s var(--n-bezier);
 line-height: var(--n-line-height);
 margin-bottom: 16px;
 color: var(--n-title-text-color);
 `),a("descriptions-table-wrapper",`
 transition:
 background-color .3s var(--n-bezier),
 border-color .3s var(--n-bezier);
 `,[a("descriptions-table",`
 width: 100%;
 border-collapse: separate;
 border-spacing: 0;
 box-sizing: border-box;
 `,[a("descriptions-table-row",`
 box-sizing: border-box;
 transition: border-color .3s var(--n-bezier);
 `,[a("descriptions-table-header",`
 font-weight: var(--n-th-font-weight);
 line-height: var(--n-line-height);
 display: table-cell;
 box-sizing: border-box;
 color: var(--n-th-text-color);
 transition:
 color .3s var(--n-bezier),
 background-color .3s var(--n-bezier),
 border-color .3s var(--n-bezier);
 `),a("descriptions-table-content",`
 vertical-align: top;
 line-height: var(--n-line-height);
 display: table-cell;
 box-sizing: border-box;
 color: var(--n-td-text-color);
 transition:
 color .3s var(--n-bezier),
 background-color .3s var(--n-bezier),
 border-color .3s var(--n-bezier);
 `,[U("content",`
 transition: color .3s var(--n-bezier);
 display: inline-block;
 color: var(--n-td-text-color);
 `)]),U("label",`
 font-weight: var(--n-th-font-weight);
 transition: color .3s var(--n-bezier);
 display: inline-block;
 margin-right: 14px;
 color: var(--n-th-text-color);
 `)])])])]),a("descriptions-table-wrapper",`
 --n-merged-th-color: var(--n-th-color);
 --n-merged-td-color: var(--n-td-color);
 --n-merged-border-color: var(--n-border-color);
 `),la(a("descriptions-table-wrapper",`
 --n-merged-th-color: var(--n-th-color-modal);
 --n-merged-td-color: var(--n-td-color-modal);
 --n-merged-border-color: var(--n-border-color-modal);
 `)),ia(a("descriptions-table-wrapper",`
 --n-merged-th-color: var(--n-th-color-popover);
 --n-merged-td-color: var(--n-td-color-popover);
 --n-merged-border-color: var(--n-border-color-popover);
 `))]);const or="DESCRIPTION_ITEM_FLAG";function sr(e){return typeof e=="object"&&e&&!Array.isArray(e)?e.type&&e.type.DESCRIPTION_ITEM_FLAG:!1}const lr=["colspan"],ir=["colspan"],dr=["colspan"],cr=["colspan"],br={...Pe.props,title:String,column:{type:Number,default:3},columns:Number,labelPlacement:{type:String,default:"top"},labelAlign:{type:String,default:"left"},separator:{type:String,default:":"},size:String,bordered:Boolean,labelClass:String,labelStyle:[Object,String],contentClass:String,contentStyle:[Object,String]};var ot=le({name:"Descriptions",props:br,slots:Object,setup(e){const{mergedClsPrefixRef:o,inlineThemeDisabled:c,mergedComponentPropsRef:v}=bt(e),u=re(()=>e.size||v?.value?.Descriptions?.size||"medium"),$=Pe("Descriptions","-descriptions",nr,ca,e,o),n=re(()=>{const{bordered:m}=e,_=u.value,{common:{cubicBezierEaseInOut:D},self:{titleTextColor:y,thColor:j,thColorModal:H,thColorPopover:W,thTextColor:B,thFontWeight:h,tdTextColor:P,tdColor:Z,tdColorModal:G,tdColorPopover:X,borderColor:S,borderColorModal:s,borderColorPopover:z,borderRadius:Y,lineHeight:N,[ae("fontSize",_)]:ne,[ae(m?"thPaddingBordered":"thPadding",_)]:Q,[ae(m?"tdPaddingBordered":"tdPadding",_)]:A}}=$.value;return{"--n-title-text-color":y,"--n-th-padding":Q,"--n-td-padding":A,"--n-font-size":ne,"--n-bezier":D,"--n-th-font-weight":h,"--n-line-height":N,"--n-th-text-color":B,"--n-td-text-color":P,"--n-th-color":j,"--n-th-color-modal":H,"--n-th-color-popover":W,"--n-td-color":Z,"--n-td-color-modal":G,"--n-td-color-popover":X,"--n-border-radius":Y,"--n-border-color":S,"--n-border-color-modal":s,"--n-border-color-popover":z}}),g=c?ft("descriptions",re(()=>{let m="";const{bordered:_}=e;return _&&(m+="a"),m+=u.value[0],m}),n,e):void 0;return{mergedClsPrefix:o,cssVars:c?void 0:n,themeClass:g?.themeClass,onRender:g?.onRender,compitableColumn:Ne(e,["columns","column"]),inlineThemeDisabled:c,mergedSize:u}},render(){const e=this.$slots.default,o=e?ke(e()):[];o.length;const{contentClass:c,labelClass:v,compitableColumn:u,labelPlacement:$,labelAlign:n,mergedSize:g,bordered:m,title:_,cssVars:D,mergedClsPrefix:y,separator:j,onRender:H}=this;H?.();const W=o.filter(h=>sr(h)),B=W.reduce((h,P,Z)=>{const G=P.props||{},X=W.length-1===Z,S=["label"in G?G.label:nt(P,"label")],s=[nt(P)],z=G.span||1,Y=h.span;h.span+=z;const N=G.labelStyle||G["label-style"]||this.labelStyle,ne=G.contentStyle||G["content-style"]||this.contentStyle;if($==="left")m?h.row.push((i(),x("th",{key:1,class:R([`${y}-descriptions-table-header`,v]),colspan:1,style:te(N)},[C(()=>S)],6)),(i(),x("td",{key:2,class:R([`${y}-descriptions-table-content`,c]),colspan:X?(u-Y)*2+1:z*2-1,style:te(ne)},[C(()=>s)],14,lr))):h.row.push((i(),x("td",{key:3,class:R(`${y}-descriptions-table-content`),colspan:X?(u-Y)*2:z*2},[L("span",{class:R([`${y}-descriptions-table-content__label`,v]),style:te(N)},[C(()=>[...S,j&&(i(),x("span",{key:4,class:R(`${y}-descriptions-separator`)},[C(()=>j)],2))])],6),L("span",{class:R([`${y}-descriptions-table-content__content`,c]),style:te(ne)},[C(()=>s)],6)],10,ir)));else{const Q=X?(u-Y)*2:z*2;h.row.push((i(),x("th",{key:5,class:R([`${y}-descriptions-table-header`,v]),colspan:Q,style:te(N)},[C(()=>S)],14,dr))),h.secondRow.push((i(),x("td",{key:6,class:R([`${y}-descriptions-table-content`,c]),colspan:Q,style:te(ne)},[C(()=>s)],14,cr)))}return(h.span>=u||X)&&(h.span=0,h.row.length&&(h.rows.push(h.row),h.row=[]),$!=="left"&&h.secondRow.length&&(h.rows.push(h.secondRow),h.secondRow=[])),h},{span:0,row:[],secondRow:[],rows:[]}).rows.map(h=>(i(),x("tr",{class:R(`${y}-descriptions-table-row`)},[C(()=>h)],2)));return i(),x("div",{style:te(D),class:R([`${y}-descriptions`,this.themeClass,`${y}-descriptions--${$}-label-placement`,`${y}-descriptions--${n}-label-align`,`${y}-descriptions--${g}-size`,m&&`${y}-descriptions--bordered`])},[_||this.$slots.header?(i(),x("div",{key:0,class:R(`${y}-descriptions-header`)},[C(()=>_||za(this,"header"))],2)):C(()=>null),L("div",{class:R(`${y}-descriptions-table-wrapper`)},[L("table",{class:R(`${y}-descriptions-table`)},[L("tbody",null,[C(()=>$==="top"&&(i(),x("tr",{class:R(`${y}-descriptions-table-row`),style:{visibility:"collapse"}},[C(()=>da(u*2,(i(),x("td"))))],2))),C(()=>B)])],2)],2)],6)}});const fr={label:String,span:{type:Number,default:1},labelClass:String,labelStyle:[Object,String],contentClass:String,contentStyle:[Object,String]};var V=le({name:"DescriptionsItem",[or]:!0,props:fr,slots:Object,render(){return null}});const Ve=ba("n-tabs"),ht={tab:[String,Number,Object,Function],name:{type:[String,Number],required:!0},disabled:Boolean,displayDirective:{type:String,default:"if"},closable:{type:Boolean,default:void 0},tabProps:Object,label:[String,Number,Object,Function]};var ve=le({__TAB_PANE__:!0,name:"TabPane",alias:["TabPanel"],props:ht,slots:Object,setup(e){const o=ut(Ve,null);return o||fa("tab-pane","`n-tab-pane` must be placed inside `n-tabs`."),{style:o.paneStyleRef,class:o.paneClassRef,mergedClsPrefix:o.mergedClsPrefixRef}},render(){return i(),x("div",{class:R([`${this.mergedClsPrefix}-tab-pane`,this.class]),style:te(this.style)},[C(()=>this.$slots.default?.())],6)}});const ur=["data-name","data-disabled"],pr={internalLeftPadded:Boolean,internalAddable:Boolean,internalCreatedByPane:Boolean,...va(ht,["displayDirective"])};var Fe=le({__TAB__:!0,inheritAttrs:!1,name:"Tab",props:pr,setup(e){const{mergedClsPrefixRef:o,valueRef:c,typeRef:v,closableRef:u,tabStyleRef:$,addTabStyleRef:n,tabClassRef:g,addTabClassRef:m,tabChangeIdRef:_,onBeforeLeaveRef:D,triggerRef:y,handleAdd:j,activateTab:H,handleClose:W}=ut(Ve);return{trigger:y,mergedClosable:re(()=>{if(e.internalAddable)return!1;const{closable:B}=e;return B===void 0?u.value:B}),style:$,addStyle:n,tabClass:g,addTabClass:m,clsPrefix:o,value:c,type:v,handleClose(B){B.stopPropagation(),!e.disabled&&W(e.name)},activateTab(){if(e.disabled)return;if(e.internalAddable){j();return}const{name:B}=e,h=++_.id;if(B!==c.value){const{value:P}=D;P?Promise.resolve(P(e.name,c.value)).then(Z=>{Z&&_.id===h&&H(B)}):H(B)}}}},render(){const{internalAddable:e,clsPrefix:o,name:c,disabled:v,label:u,tab:$,value:n,mergedClosable:g,trigger:m,$slots:{default:_}}=this,D=u??$;return i(),x("div",{class:R(`${o}-tabs-tab-wrapper`)},[this.internalLeftPadded?(i(),x("div",{key:0,class:R(`${o}-tabs-tab-pad`)},null,2)):C(()=>null),(i(),x("div",Te({key:c,"data-name":c,"data-disabled":v?!0:void 0},Te({class:[`${o}-tabs-tab`,n===c&&`${o}-tabs-tab--active`,v&&`${o}-tabs-tab--disabled`,g&&`${o}-tabs-tab--closable`,e&&`${o}-tabs-tab--addable`,e?this.addTabClass:this.tabClass],onClick:m==="click"?this.activateTab:void 0,onMouseenter:m==="hover"?this.activateTab:void 0,style:e?this.addStyle:this.style},this.internalCreatedByPane?this.tabProps||{}:this.$attrs)),[L("span",{class:R(`${o}-tabs-tab__label`)},[e?(i(),x(ee,{key:0},[L("div",{class:R(`${o}-tabs-tab__height-placeholder`)}," ",2),(i(),K(pt,{clsPrefix:o},{default:()=>(i(),K($a))},1032,["clsPrefix"]))],64)):(i(),x(ee,{key:1},[_?(i(),x(ee,{key:0},[C(()=>_())],64)):(i(),x(ee,{key:1},[typeof D=="object"?(i(),x(ee,{key:0},[C(()=>D)],64)):(i(),x(ee,{key:1},[C(()=>ua(D??c))],64))],64))],64))],2),g&&this.type==="card"?(i(),K(pa,{key:0,clsPrefix:o,class:R(`${o}-tabs-tab__close`),onClick:this.handleClose,disabled:v},null,8,["clsPrefix","class","onClick","disabled"])):C(()=>null)],16,ur))],2)}}),vr=a("tabs",`
 box-sizing: border-box;
 width: 100%;
 display: flex;
 flex-direction: column;
 transition:
 background-color .3s var(--n-bezier),
 border-color .3s var(--n-bezier);
`,[k("&.transition-disabled",[a("tabs-tab",`
 transition: none !important;
 `),a("tabs-nav-scroll-content",`
 transition: none !important;
 `),a("tabs-tab-pad",`
 transition: none !important;
 `)]),p("segment-type",[a("tabs-rail",[k("&.transition-disabled",[a("tabs-capsule",`
 transition: none;
 `)])])]),p("top",[a("tab-pane",`
 padding: var(--n-pane-padding-top) var(--n-pane-padding-right) var(--n-pane-padding-bottom) var(--n-pane-padding-left);
 `)]),p("left",[a("tab-pane",`
 padding: var(--n-pane-padding-right) var(--n-pane-padding-bottom) var(--n-pane-padding-left) var(--n-pane-padding-top);
 `)]),p("left, right",`
 flex-direction: row;
 `,[a("tabs-bar",`
 width: 2px;
 right: 0;
 transition:
 top .2s var(--n-bezier),
 max-height .2s var(--n-bezier),
 background-color .3s var(--n-bezier);
 `),a("tabs-tab",`
 padding: var(--n-tab-padding-vertical); 
 `)]),p("right",`
 flex-direction: row-reverse;
 `,[a("tab-pane",`
 padding: var(--n-pane-padding-left) var(--n-pane-padding-top) var(--n-pane-padding-right) var(--n-pane-padding-bottom);
 `),a("tabs-bar",`
 left: 0;
 `)]),p("bottom",`
 flex-direction: column-reverse;
 justify-content: flex-end;
 `,[a("tab-pane",`
 padding: var(--n-pane-padding-bottom) var(--n-pane-padding-right) var(--n-pane-padding-top) var(--n-pane-padding-left);
 `),a("tabs-bar",`
 top: 0;
 `)]),a("tabs-rail",`
 position: relative;
 padding: 3px;
 border-radius: var(--n-tab-border-radius);
 width: 100%;
 background-color: var(--n-color-segment);
 transition: background-color .3s var(--n-bezier);
 display: flex;
 align-items: center;
 `,[a("tabs-capsule",`
 border-radius: var(--n-tab-border-radius);
 position: absolute;
 left: 0;
 top: 0;
 pointer-events: none;
 background-color: var(--n-tab-color-segment);
 box-shadow: 0 1px 3px 0 rgba(0, 0, 0, .08);
 transition: transform 0.3s var(--n-bezier);
 `),a("tabs-tab-wrapper",`
 flex-basis: 0;
 flex-grow: 1;
 display: flex;
 align-items: center;
 justify-content: center;
 `,[a("tabs-tab",`
 overflow: hidden;
 border-radius: var(--n-tab-border-radius);
 width: 100%;
 display: flex;
 align-items: center;
 justify-content: center;
 `,[p("active",`
 font-weight: var(--n-font-weight-strong);
 color: var(--n-tab-text-color-active);
 `),k("&:hover",`
 color: var(--n-tab-text-color-hover);
 `)])])]),p("flex",[a("tabs-nav",`
 width: 100%;
 position: relative;
 `,[a("tabs-wrapper",`
 width: 100%;
 `,[a("tabs-tab",`
 margin-right: 0;
 `)])])]),a("tabs-nav",`
 box-sizing: border-box;
 line-height: 1.5;
 display: flex;
 transition: border-color .3s var(--n-bezier);
 `,[U("prefix, suffix",`
 display: flex;
 align-items: center;
 `),U("prefix","padding-right: 16px;"),U("suffix","padding-left: 16px;")]),p("top, bottom",[k(">",[a("tabs-nav",[a("tabs-nav-scroll-wrapper",[k("&::before",`
 top: 0;
 bottom: 0;
 left: 0;
 width: 20px;
 `),k("&::after",`
 top: 0;
 bottom: 0;
 right: 0;
 width: 20px;
 `),p("shadow-start",[k("&::before",`
 box-shadow: inset 10px 0 8px -8px rgba(0, 0, 0, .12);
 `)]),p("shadow-end",[k("&::after",`
 box-shadow: inset -10px 0 8px -8px rgba(0, 0, 0, .12);
 `)])])])])]),p("left, right",[a("tabs-nav-scroll-content",`
 flex-direction: column;
 `),k(">",[a("tabs-nav",[a("tabs-nav-scroll-wrapper",[k("&::before",`
 top: 0;
 left: 0;
 right: 0;
 height: 20px;
 `),k("&::after",`
 bottom: 0;
 left: 0;
 right: 0;
 height: 20px;
 `),p("shadow-start",[k("&::before",`
 box-shadow: inset 0 10px 8px -8px rgba(0, 0, 0, .12);
 `)]),p("shadow-end",[k("&::after",`
 box-shadow: inset 0 -10px 8px -8px rgba(0, 0, 0, .12);
 `)])])])])]),a("tabs-nav-scroll-wrapper",`
 flex: 1;
 position: relative;
 overflow: hidden;
 `,[a("tabs-nav-y-scroll",`
 height: 100%;
 width: 100%;
 overflow-y: auto; 
 scrollbar-width: none;
 `,[k("&::-webkit-scrollbar, &::-webkit-scrollbar-track-piece, &::-webkit-scrollbar-thumb",`
 width: 0;
 height: 0;
 display: none;
 `)]),k("&::before, &::after",`
 transition: box-shadow .3s var(--n-bezier);
 pointer-events: none;
 content: "";
 position: absolute;
 z-index: 1;
 `),k("&.transition-disabled",[k("&::before, &::after",`
 transition: none;
 `)])]),a("tabs-nav-scroll-content",`
 display: flex;
 position: relative;
 min-width: 100%;
 min-height: 100%;
 width: fit-content;
 box-sizing: border-box;
 `),a("tabs-wrapper",`
 display: inline-flex;
 flex-wrap: nowrap;
 position: relative;
 `),a("tabs-tab-wrapper",`
 display: flex;
 flex-wrap: nowrap;
 flex-shrink: 0;
 flex-grow: 0;
 `),a("tabs-tab",`
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
 `,[p("disabled",{cursor:"not-allowed"}),U("close",`
 margin-inline-start: 6px;
 transition:
 background-color .3s var(--n-bezier),
 color .3s var(--n-bezier);
 `),U("label",`
 display: flex;
 align-items: center;
 z-index: 1;
 `)]),a("tabs-bar",`
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
 `,[k("&.transition-disabled",`
 transition: none;
 `),p("disabled",`
 background-color: var(--n-tab-text-color-disabled)
 `)]),a("tabs-pane-wrapper",`
 position: relative;
 overflow: hidden;
 transition: max-height .2s var(--n-bezier);
 `),a("tab-pane",`
 color: var(--n-pane-text-color);
 width: 100%;
 transition:
 color .3s var(--n-bezier),
 background-color .3s var(--n-bezier),
 opacity .2s var(--n-bezier);
 left: 0;
 right: 0;
 top: 0;
 `,[k("&.next-transition-leave-active, &.prev-transition-leave-active, &.next-transition-enter-active, &.prev-transition-enter-active",`
 transition:
 color .3s var(--n-bezier),
 background-color .3s var(--n-bezier),
 transform .2s var(--n-bezier),
 opacity .2s var(--n-bezier);
 `),k("&.next-transition-leave-active, &.prev-transition-leave-active",`
 position: absolute;
 `),k("&.next-transition-enter-from, &.prev-transition-leave-to",`
 transform: translateX(32px);
 opacity: 0;
 `),k("&.next-transition-leave-to, &.prev-transition-enter-from",`
 transform: translateX(-32px);
 opacity: 0;
 `),k("&.next-transition-leave-from, &.next-transition-enter-to, &.prev-transition-leave-from, &.prev-transition-enter-to",`
 transform: translateX(0);
 opacity: 1;
 `)]),a("tabs-tab-pad",`
 box-sizing: border-box;
 width: var(--n-tab-gap);
 flex-grow: 0;
 flex-shrink: 0;
 `),p("line-type, bar-type",[a("tabs-tab",`
 font-weight: var(--n-tab-font-weight);
 box-sizing: border-box;
 vertical-align: bottom;
 `,[k("&:hover",{color:"var(--n-tab-text-color-hover)"}),p("active",`
 color: var(--n-tab-text-color-active);
 font-weight: var(--n-tab-font-weight-active);
 `),p("disabled",{color:"var(--n-tab-text-color-disabled)"})])]),a("tabs-nav",[U("prefix, suffix",`
 border-color: var(--n-tab-border-color);
 `),a("tabs-nav-scroll-content",`
 border-color: var(--n-tab-border-color);
 `),p("line-type",[p("top",[U("prefix, suffix",`
 border-bottom: 1px solid var(--n-tab-border-color);
 `),a("tabs-nav-scroll-content",`
 border-bottom: 1px solid var(--n-tab-border-color);
 `),a("tabs-bar",`
 bottom: -1px;
 `)]),p("left",[U("prefix, suffix",`
 border-right: 1px solid var(--n-tab-border-color);
 `),a("tabs-nav-scroll-content",`
 border-right: 1px solid var(--n-tab-border-color);
 `),a("tabs-bar",`
 right: -1px;
 `)]),p("right",[U("prefix, suffix",`
 border-left: 1px solid var(--n-tab-border-color);
 `),a("tabs-nav-scroll-content",`
 border-left: 1px solid var(--n-tab-border-color);
 `),a("tabs-bar",`
 left: -1px;
 `)]),p("bottom",[U("prefix, suffix",`
 border-top: 1px solid var(--n-tab-border-color);
 `),a("tabs-nav-scroll-content",`
 border-top: 1px solid var(--n-tab-border-color);
 `),a("tabs-bar",`
 top: -1px;
 `)]),U("prefix, suffix",`
 transition: border-color .3s var(--n-bezier);
 `),a("tabs-nav-scroll-content",`
 transition: border-color .3s var(--n-bezier);
 `),a("tabs-bar",`
 border-radius: 0;
 `)]),p("card-type",[U("prefix, suffix",`
 transition: border-color .3s var(--n-bezier);
 `),a("tabs-pad",`
 flex-grow: 1;
 transition: border-color .3s var(--n-bezier);
 `),a("tabs-tab-pad",`
 transition: border-color .3s var(--n-bezier);
 `),a("tabs-tab",`
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
 `,[p("addable",`
 padding-left: 8px;
 padding-right: 8px;
 font-size: 16px;
 justify-content: center;
 `,[U("height-placeholder",`
 width: 0;
 font-size: var(--n-tab-font-size);
 `),ct("disabled",[k("&:hover",`
 color: var(--n-tab-text-color-hover);
 `)])]),p("closable","padding-inline-end: 8px;"),p("active",`
 background-color: #0000;
 font-weight: var(--n-tab-font-weight-active);
 color: var(--n-tab-text-color-active);
 `),p("disabled","color: var(--n-tab-text-color-disabled);")])]),p("left, right",`
 flex-direction: column; 
 `,[U("prefix, suffix",`
 padding: var(--n-tab-padding-vertical);
 `),a("tabs-wrapper",`
 flex-direction: column;
 `),a("tabs-tab-wrapper",`
 flex-direction: column;
 `,[a("tabs-tab-pad",`
 height: var(--n-tab-gap-vertical);
 width: 100%;
 `)])]),p("top",[p("card-type",[a("tabs-scroll-padding","border-bottom: 1px solid var(--n-tab-border-color);"),U("prefix, suffix",`
 border-bottom: 1px solid var(--n-tab-border-color);
 `),a("tabs-tab",`
 border-top-left-radius: var(--n-tab-border-radius);
 border-top-right-radius: var(--n-tab-border-radius);
 `,[p("active",`
 border-bottom: 1px solid #0000;
 `)]),a("tabs-tab-pad",`
 border-bottom: 1px solid var(--n-tab-border-color);
 `),a("tabs-pad",`
 border-bottom: 1px solid var(--n-tab-border-color);
 `)])]),p("left",[p("card-type",[a("tabs-scroll-padding","border-right: 1px solid var(--n-tab-border-color);"),U("prefix, suffix",`
 border-right: 1px solid var(--n-tab-border-color);
 `),a("tabs-tab",`
 border-top-left-radius: var(--n-tab-border-radius);
 border-bottom-left-radius: var(--n-tab-border-radius);
 `,[p("active",`
 border-right: 1px solid #0000;
 `)]),a("tabs-tab-pad",`
 border-right: 1px solid var(--n-tab-border-color);
 `),a("tabs-pad",`
 border-right: 1px solid var(--n-tab-border-color);
 `)])]),p("right",[p("card-type",[a("tabs-scroll-padding","border-left: 1px solid var(--n-tab-border-color);"),U("prefix, suffix",`
 border-left: 1px solid var(--n-tab-border-color);
 `),a("tabs-tab",`
 border-top-right-radius: var(--n-tab-border-radius);
 border-bottom-right-radius: var(--n-tab-border-radius);
 `,[p("active",`
 border-left: 1px solid #0000;
 `)]),a("tabs-tab-pad",`
 border-left: 1px solid var(--n-tab-border-color);
 `),a("tabs-pad",`
 border-left: 1px solid var(--n-tab-border-color);
 `)])]),p("bottom",[p("card-type",[a("tabs-scroll-padding","border-top: 1px solid var(--n-tab-border-color);"),U("prefix, suffix",`
 border-top: 1px solid var(--n-tab-border-color);
 `),a("tabs-tab",`
 border-bottom-left-radius: var(--n-tab-border-radius);
 border-bottom-right-radius: var(--n-tab-border-radius);
 `,[p("active",`
 border-top: 1px solid #0000;
 `)]),a("tabs-tab-pad",`
 border-top: 1px solid var(--n-tab-border-color);
 `),a("tabs-pad",`
 border-top: 1px solid var(--n-tab-border-color);
 `)])])]),a("tabs-scroll-button",[p("start",`
 padding-left: 10px;
 padding-right: 6px;
 `),p("end",`
 padding-right: 10px;
 padding-left: 6px;
 `),p("up",`
 padding-bottom: 10px;
 `),p("down",`
 padding-top: 10px;
 `)])]),st=le({name:"TabsButton",props:{type:{type:String,default:"next"},mergedClsPrefix:{type:String,required:!0},vertical:Boolean,disabled:Boolean,rtl:Boolean,theme:Object,themeOverrides:Object,onClick:Function},setup(e){return{handleClick:()=>{e.disabled||e.onClick?.(e.type)}}},render(){const{mergedClsPrefix:e,disabled:o,type:c,vertical:v,rtl:u,theme:$,themeOverrides:n,handleClick:g}=this,m=c==="next",_=v?m:u?!m:m;return i(),K(he,{text:!0,disabled:o,size:"small",theme:$,themeOverrides:n,onClick:g,class:R([`${e}-tabs-scroll-button`,!v&&c==="prev"&&`${e}-tabs-scroll-button--start`,!v&&c==="next"&&`${e}-tabs-scroll-button--end`,v&&c==="prev"&&`${e}-tabs-scroll-button--up`,v&&c==="next"&&`${e}-tabs-scroll-button--down`])},{icon:()=>(i(),K(pt,{clsPrefix:e,style:te(v?{transform:"rotate(90deg)"}:void 0)},{default:()=>_?(i(),K(Ba,{key:1})):(i(),K(rr,{key:2}))},1032,["clsPrefix","style"]))},1032,["disabled","theme","themeOverrides","onClick","class"])}});const He=er,hr={...Pe.props,value:[String,Number],defaultValue:[String,Number],trigger:{type:String,default:"click"},type:{type:String,default:"bar"},closable:Boolean,justifyContent:String,size:String,placement:{type:String,default:"top"},tabStyle:[String,Object],tabClass:String,addTabStyle:[String,Object],addTabClass:String,barWidth:Number,paneClass:String,paneStyle:[String,Object],paneWrapperClass:String,paneWrapperStyle:[String,Object],addable:[Boolean,Object],tabsPadding:{type:Number,default:0},animated:Boolean,onBeforeLeave:Function,onAdd:Function,"onUpdate:value":[Function,Array],onUpdateValue:[Function,Array],onClose:[Function,Array],labelSize:String,activeName:[String,Number],onActiveNameChange:[Function,Array],showScrollButton:Boolean,centerActiveTab:Boolean};var gr=le({name:"Tabs",props:hr,slots:Object,setup(e,{slots:o}){const{mergedClsPrefixRef:c,inlineThemeDisabled:v,mergedComponentPropsRef:u,mergedRtlRef:$}=bt(e),n=ha("Tabs",$,c),g=re(()=>{const{placement:t}=e;return t==="start"?n?.value?"right":"left":t==="end"?n?.value?"left":"right":t}),m=Pe("Tabs","-tabs",vr,xa,e,c),_=M(null),D=M(null),y=M(null),j=M(null),H=M(null),W=M(null),B=M(null),h=M(!0),P=M(!0),Z=Ne(e,["labelSize","size"]),G=re(()=>{if(Z.value)return Z.value;const t=u?.value?.Tabs?.size;return t||"medium"}),X=Ne(e,["activeName","value"]),S=M(X.value??e.defaultValue??(o.default?ke(o.default())[0]?.props?.name:null)),s=Aa(X,S),z={id:0},Y=re(()=>{if(!(!e.justifyContent||e.type==="card"))return{display:"flex",justifyContent:e.justifyContent}});ue(s,()=>{z.id=0,J(),pe(()=>{Be()})});function N(){const{value:t}=s;return t===null?null:_.value?.querySelector(`[data-name="${t}"]`)}function ne(t){if(e.type==="card")return;const{value:r}=y;if(!r)return;const d=r.style.opacity==="0";if(t){const w=`${c.value}-tabs-bar--disabled`,{barWidth:E}=e,F=g.value;if(t.dataset.disabled==="true"?r.classList.add(w):r.classList.remove(w),["top","bottom"].includes(F)){if(A(["top","maxHeight","height"]),typeof E=="number"&&t.offsetWidth>=E){const O=Math.floor((t.offsetWidth-E)/2)+t.offsetLeft;r.style.left=`${O}px`,r.style.maxWidth=`${E}px`}else r.style.left=`${t.offsetLeft}px`,r.style.maxWidth=`${t.offsetWidth}px`;r.style.width="8192px",d&&(r.style.transition="none"),r.offsetWidth,d&&(r.style.transition="",r.style.opacity="1")}else{if(A(["left","maxWidth","width"]),typeof E=="number"&&t.offsetHeight>=E){const O=Math.floor((t.offsetHeight-E)/2)+t.offsetTop;r.style.top=`${O}px`,r.style.maxHeight=`${E}px`}else r.style.top=`${t.offsetTop}px`,r.style.maxHeight=`${t.offsetHeight}px`;r.style.height="8192px",d&&(r.style.transition="none"),r.offsetHeight,d&&(r.style.transition="",r.style.opacity="1")}}}function Q(){if(e.type==="card")return;const{value:t}=y;t&&(t.style.opacity="0")}function A(t){const{value:r}=y;if(r)for(const d of t)r.style[d]=""}function J(){if(e.type==="card")return;const t=N();t?ne(t):Q()}function ce(t,r,d,w){const E=t.getBoundingClientRect(),F=r.getBoundingClientRect(),O=d?"left":"top",q=d?"right":"bottom";let oe=0;w?oe=(F[O]+F[q])/2-(E[O]+E[q])/2:F[O]<E[O]?oe=F[O]-E[O]:F[q]>E[q]&&(oe=F[q]-E[q]),oe!==0&&t.scrollBy({[O]:oe,behavior:"smooth"})}function Be(){const t=["top","bottom"].includes(g.value),r=N();if(r)if(t){const d=W.value?.$el;if(!d)return;ce(d,r,t,e.centerActiveTab)}else{const{value:d}=B;if(!d)return;ce(d,r,t,e.centerActiveTab)}}const ge=M(null);let Ae=0,ie=null;function gt(t){const r=ge.value;if(r){Ae=t.getBoundingClientRect().height;const d=`${Ae}px`,w=()=>{r.style.height=d,r.style.maxHeight=d};ie?(w(),ie(),ie=null):ie=w}}function mt(t){const r=ge.value;if(r){const d=t.getBoundingClientRect().height,w=()=>{document.body.offsetHeight,r.style.maxHeight=`${d}px`,r.style.height=`${Math.max(Ae,d)}px`};ie?(ie(),ie=null,w()):ie=w}}function xt(){const t=ge.value;if(t){t.style.maxHeight="",t.style.height="";const{paneWrapperStyle:r}=e;if(typeof r=="string")t.style.cssText=r;else if(r){const{maxHeight:d,height:w}=r;d!==void 0&&(t.style.maxHeight=d),w!==void 0&&(t.style.height=w)}}}const Ue={value:[]},Ge=M("next");function yt(t){const r=s.value;let d="next";for(const w of Ue.value){if(w===r)break;if(w===t){d="prev";break}}Ge.value=d,Ct(t)}function Ct(t){const{onActiveNameChange:r,onUpdateValue:d,"onUpdate:value":w}=e;r&&_e(r,t),d&&_e(d,t),w&&_e(w,t),S.value=t}function wt(t){const{onClose:r}=e;r&&_e(r,t)}function _t(t){if(["top","bottom"].includes(g.value)){const{value:r}=W;if(!r)return;const d=r.$el;if(!d)return;const w=d.offsetWidth,E=!!n?.value,F=t==="next"?w:-w;d.scrollBy({left:E?-F:F,behavior:"smooth"})}else{const{value:r}=B;if(!r)return;const d=r.offsetHeight,w=t==="next"?r.scrollTop+d:r.scrollTop-d;r.scrollTo({top:w,left:0,behavior:"smooth"})}}let Le=!0;function Ee(){const{value:t}=y;if(!t)return;Le&&(Le=!1);const r="transition-disabled";t.classList.add(r),J(),t.classList.remove(r)}const be=M(null);function me({transitionDisabled:t}){const r=_.value;if(!r)return;t&&r.classList.add("transition-disabled");const d=N();d&&be.value&&(be.value.style.width=`${d.offsetWidth}px`,be.value.style.height=`${d.offsetHeight}px`,be.value.style.transform=`translate(${d.offsetLeft}px, ${d.offsetTop}px)`,t&&be.value.offsetWidth),t&&r.classList.remove("transition-disabled")}ue([s],()=>{e.type==="segment"&&pe(()=>{me({transitionDisabled:!1})})}),vt(()=>{e.type==="segment"&&me({transitionDisabled:!0})});let Xe=0;function St(t){if(t.contentRect.width===0&&t.contentRect.height===0||Xe===t.contentRect.width)return;Xe=t.contentRect.width;const{type:r}=e;(r==="line"||r==="bar")&&(Le||e.justifyContent?.startsWith("space"))&&Ee(),r!=="segment"&&xe(qe())}const Rt=He(St,64);function Ke(){const{type:t}=e;t==="line"||t==="bar"?Ee():t==="segment"&&me({transitionDisabled:!0})}ue([()=>e.justifyContent,()=>e.size],()=>{pe(()=>{(e.type==="line"||e.type==="bar")&&Ee()})}),ue([g,()=>n?.value],()=>{pe(()=>{Ke(),xe(qe(),{instantly:!0})})}),ue(()=>e.type,()=>{pe(()=>{const t=D.value;t&&(t.classList.add("transition-disabled"),Ke(),t.offsetWidth,t.classList.remove("transition-disabled"))})});const fe=M(!1);function zt(t){const{target:r,contentRect:{width:d,height:w}}=t,E=r.parentElement.parentElement.offsetWidth,F=r.parentElement.parentElement.offsetHeight,O=g.value;if(!fe.value)O==="top"||O==="bottom"?E<d&&(fe.value=!0):F<w&&(fe.value=!0);else{const{value:q}=H;if(!q)return;O==="top"||O==="bottom"?E-d>q.$el.offsetWidth&&(fe.value=!1):F-w>q.$el.offsetHeight&&(fe.value=!1)}xe(W.value?.$el||null)}const kt=He(zt,64);function $t(){const{onAdd:t}=e;t&&t()}const Ie=M(!1);function qe(){const t=g.value;return(t==="top"||t==="bottom"?W.value?.$el:B.value)||null}function xe(t,r={instantly:!1}){if(!t)return;const d=r.instantly?j.value:null;d&&d.classList.add("transition-disabled");const w=1,E=g.value;if(E==="top"||E==="bottom"){const{scrollLeft:F,scrollWidth:O,offsetWidth:q}=t,oe=Math.abs(F);h.value=oe<=w,P.value=oe+q>=O-w,Ie.value=q<O-w}else{const{scrollTop:F,scrollHeight:O,offsetHeight:q}=t;h.value=F<=w,P.value=F+q>=O-w,Ie.value=q<O-w}d&&(d.offsetWidth,d.classList.remove("transition-disabled"))}const Tt=He(t=>{xe(t.target)},64);_a(Ve,{triggerRef:se(e,"trigger"),tabStyleRef:se(e,"tabStyle"),tabClassRef:se(e,"tabClass"),addTabStyleRef:se(e,"addTabStyle"),addTabClassRef:se(e,"addTabClass"),paneClassRef:se(e,"paneClass"),paneStyleRef:se(e,"paneStyle"),mergedClsPrefixRef:c,typeRef:se(e,"type"),closableRef:se(e,"closable"),valueRef:s,tabChangeIdRef:z,onBeforeLeaveRef:se(e,"onBeforeLeave"),activateTab:yt,handleClose:wt,handleAdd:$t}),Ea(()=>{J(),Be()}),ga(()=>{const{value:t}=j;if(!t)return;const{value:r}=c,d=`${r}-tabs-nav-scroll-wrapper--shadow-start`,w=`${r}-tabs-nav-scroll-wrapper--shadow-end`;h.value?t.classList.remove(d):t.classList.add(d),P.value?t.classList.remove(w):t.classList.add(w)});const Pt={syncBarPosition:()=>{J()},scrollToCurrentTab:()=>{Be()}},Bt=()=>{me({transitionDisabled:!0})},Ye=re(()=>{const{value:t}=G,{type:r}=e,d=`${t}${{card:"Card",bar:"Bar",line:"Line",segment:"Segment"}[r]}`,{self:{barColor:w,closeIconColor:E,closeIconColorHover:F,closeIconColorPressed:O,tabColor:q,tabBorderColor:oe,paneTextColor:At,tabFontWeight:Lt,tabBorderRadius:Et,tabFontWeightActive:It,colorSegment:Wt,fontWeightStrong:Ot,tabColorSegment:Dt,closeSize:jt,closeIconSize:Ht,closeColorHover:Mt,closeColorPressed:Nt,closeBorderRadius:Ft,[ae("panePadding",t)]:ye,[ae("tabPadding",d)]:Vt,[ae("tabPaddingVertical",d)]:Ut,[ae("tabGap",d)]:Gt,[ae("tabGap",`${d}Vertical`)]:Xt,[ae("tabTextColor",r)]:Kt,[ae("tabTextColorActive",r)]:qt,[ae("tabTextColorHover",r)]:Yt,[ae("tabTextColorDisabled",r)]:Jt,[ae("tabFontSize",t)]:Zt},common:{cubicBezierEaseInOut:Qt}}=m.value;return{"--n-bezier":Qt,"--n-color-segment":Wt,"--n-bar-color":w,"--n-tab-font-size":Zt,"--n-tab-text-color":Kt,"--n-tab-text-color-active":qt,"--n-tab-text-color-disabled":Jt,"--n-tab-text-color-hover":Yt,"--n-pane-text-color":At,"--n-tab-border-color":oe,"--n-tab-border-radius":Et,"--n-close-size":jt,"--n-close-icon-size":Ht,"--n-close-color-hover":Mt,"--n-close-color-pressed":Nt,"--n-close-border-radius":Ft,"--n-close-icon-color":E,"--n-close-icon-color-hover":F,"--n-close-icon-color-pressed":O,"--n-tab-color":q,"--n-tab-font-weight":Lt,"--n-tab-font-weight-active":It,"--n-tab-padding":Vt,"--n-tab-padding-vertical":Ut,"--n-tab-gap":Gt,"--n-tab-gap-vertical":Xt,"--n-pane-padding-left":we(ye,"left"),"--n-pane-padding-right":we(ye,"right"),"--n-pane-padding-top":we(ye,"top"),"--n-pane-padding-bottom":we(ye,"bottom"),"--n-font-weight-strong":Ot,"--n-tab-color-segment":Dt}}),Je=v?ft("tabs",re(()=>`${G.value[0]}${e.type[0]}`),Ye,e):void 0;return{mergedClsPrefix:c,mergedValue:s,renderedNames:new Set,segmentCapsuleElRef:be,tabsPaneWrapperRef:ge,tabsElRef:_,selfElRef:D,barElRef:y,addTabInstRef:H,xScrollInstRef:W,scrollWrapperElRef:j,addTabFixed:fe,tabWrapperStyle:Y,handleNavResize:Rt,mergedSize:G,handleScroll:Tt,handleTabsResize:kt,cssVars:v?void 0:Ye,themeClass:Je?.themeClass,animationDirection:Ge,renderNameListRef:Ue,yScrollElRef:B,handleSegmentResize:Bt,onAnimationBeforeLeave:gt,onAnimationEnter:mt,onAnimationAfterEnter:xt,onRender:Je?.onRender,startReachedRef:h,endReachedRef:P,isOverflow:Ie,handleButtonClick:_t,mergedTheme:m,rtlEnabled:n,mergedPlacement:g,...Pt}},render(){const{mergedClsPrefix:e,type:o,mergedPlacement:c,addTabFixed:v,addable:u,mergedSize:$,renderNameListRef:n,onRender:g,paneWrapperClass:m,paneWrapperStyle:_,startReachedRef:D,endReachedRef:y,isOverflow:j,showScrollButton:H,handleButtonClick:W,mergedTheme:B,rtlEnabled:h,$slots:{default:P,prefix:Z,suffix:G}}=this;g?.();const X=P?ke(P()).filter(A=>A.type.__TAB_PANE__===!0):[],S=P?ke(P()).filter(A=>A.type.__TAB__===!0):[],s=!S.length,z=o==="card",Y=o==="segment",N=!z&&!Y&&this.justifyContent;n.value=[];const ne=()=>{const A=(i(),x("div",{style:te(this.tabWrapperStyle),class:R(`${e}-tabs-wrapper`)},[N?C(()=>null):(i(),x("div",{key:1,class:R(`${e}-tabs-scroll-padding`),style:te(c==="top"||c==="bottom"?{width:`${this.tabsPadding}px`}:{height:`${this.tabsPadding}px`})},null,6)),s?(i(),x(ee,{key:2},[C(()=>X.map((J,ce)=>(n.value.push(J.props.name),Me((i(),K(Fe,Te(J.props,{internalCreatedByPane:!0,internalLeftPadded:ce!==0&&(!N||N==="center"||N==="start"||N==="end")}),Qe(J.children?{default:J.children.tab}:void 0),1040,["internalLeftPadded"]))))))],64)):(i(),x(ee,{key:3},[C(()=>S.map((J,ce)=>(n.value.push(J.props.name),Me(ce!==0&&!N?dt(J):J))))],64)),!v&&u&&z?(i(),x(ee,{key:4},[C(()=>it(u,(s?X.length:S.length)!==0))],64)):C(()=>null),N?C(()=>null):(i(),x("div",{key:7,class:R(`${e}-tabs-scroll-padding`),style:te({width:`${this.tabsPadding}px`})},null,6)),z?C(()=>null):(i(),x("div",{key:9,ref:"barElRef",class:R(`${e}-tabs-bar`)},null,2))],6));return i(),x("div",{ref:"tabsElRef",class:R(`${e}-tabs-nav-scroll-content`)},[z&&u?(i(),K(We,{key:0,onResize:this.handleTabsResize},{default:()=>A},1032,["onResize"])):(i(),x(ee,{key:1},[C(()=>A)],64)),z?(i(),x("div",{key:2,class:R(`${e}-tabs-pad`)},null,2)):C(()=>null)],2)},Q=Y?"top":c;return i(),x("div",{ref:"selfElRef",class:R([`${e}-tabs`,this.themeClass,`${e}-tabs--${o}-type`,`${e}-tabs--${$}-size`,N&&`${e}-tabs--flex`,`${e}-tabs--${Q}`,h&&`${e}-tabs--rtl`]),style:te(this.cssVars)},[L("div",{class:R([`${e}-tabs-nav--${o}-type`,`${e}-tabs-nav--${Q}`,`${e}-tabs-nav`])},[C(()=>Ze(Z,A=>A&&(i(),x("div",{class:R(`${e}-tabs-nav__prefix`)},[C(()=>A)],2)))),Y?(i(),K(We,{key:0,onResize:this.handleSegmentResize},{default:()=>(i(),x("div",{class:R(`${e}-tabs-rail`),ref:"tabsElRef"},[L("div",{class:R(`${e}-tabs-capsule`),ref:"segmentCapsuleElRef"},[L("div",{class:R(`${e}-tabs-wrapper`)},[L("div",{class:R(`${e}-tabs-tab`)},null,2)],2)],2),s?(i(),x(ee,{key:0},[C(()=>X.map((A,J)=>(n.value.push(A.props.name),i(),K(Fe,Te(A.props,{internalCreatedByPane:!0,internalLeftPadded:J!==0}),Qe(A.children?{default:A.children.tab}:void 0),1040,["internalLeftPadded"]))))],64)):(i(),x(ee,{key:1},[C(()=>S.map((A,J)=>(n.value.push(A.props.name),J===0?A:dt(A))))],64))],2))},1032,["onResize"])):(i(),x(ee,{key:1},[C(()=>H&&j&&(i(),K(st,{mergedClsPrefix:e,type:"prev",vertical:Q==="left"||Q==="right",disabled:D,rtl:!!h,theme:B.peers.Button,themeOverrides:B.peerOverrides.Button,onClick:W},null,8,["mergedClsPrefix","vertical","disabled","rtl","theme","themeOverrides","onClick"]))),(i(),K(We,{onResize:this.handleNavResize},{default:()=>(i(),x("div",{class:R(`${e}-tabs-nav-scroll-wrapper`),ref:"scrollWrapperElRef"},[["top","bottom"].includes(Q)?(i(),K(ar,{key:0,ref:"xScrollInstRef",onScroll:this.handleScroll},{default:ne},1032,["onScroll"])):(i(),x("div",{key:1,class:R(`${e}-tabs-nav-y-scroll`),onScroll:this.handleScroll,ref:"yScrollElRef"},[C(()=>ne())],42,["onScroll"]))],2))},1032,["onResize"])),C(()=>H&&j&&(i(),K(st,{mergedClsPrefix:e,type:"next",vertical:Q==="left"||Q==="right",disabled:y,rtl:!!h,theme:B.peers.Button,themeOverrides:B.peerOverrides.Button,onClick:W},null,8,["mergedClsPrefix","vertical","disabled","rtl","theme","themeOverrides","onClick"])))],64)),v&&u&&z?(i(),x(ee,{key:2},[C(()=>it(u,!0))],64)):C(()=>null),C(()=>Ze(G,A=>A&&(i(),x("div",{class:R(`${e}-tabs-nav__suffix`)},[C(()=>A)],2))))],2),C(()=>s&&(this.animated&&(Q==="top"||Q==="bottom")?(i(),x("div",{key:1,ref:"tabsPaneWrapperRef",style:te(_),class:R([`${e}-tabs-pane-wrapper`,m])},[C(()=>lt(X,this.mergedValue,this.renderedNames,this.onAnimationBeforeLeave,this.onAnimationEnter,this.onAnimationAfterEnter,this.animationDirection))],6)):lt(X,this.mergedValue,this.renderedNames)))],6)}});function lt(e,o,c,v,u,$,n){const g=[];return e.forEach(m=>{const{name:_,displayDirective:D,"display-directive":y}=m.props,j=W=>D===W||y===W,H=o===_;if(m.key!==void 0&&(m.key=_),H||j("show")||j("show:lazy")&&c.has(_)){c.has(_)||c.add(_);const W=!j("if");g.push(W?ma(m,[[ya,H]]):m)}}),n?(i(),K(Ca,{name:`${n}-transition`,onBeforeLeave:v,onEnter:u,onAfterEnter:$},{default:()=>g},1032,["name","onBeforeLeave","onEnter","onAfterEnter"])):g}function it(e,o){return i(),K(Fe,{ref:"addTabInstRef",key:"__addable",name:"__addable",internalCreatedByPane:!0,internalAddable:!0,internalLeftPadded:o,disabled:typeof e=="object"&&e.disabled},null,8,["internalLeftPadded","disabled"])}function dt(e){const o=wa(e);return o.props?o.props.internalLeftPadded=!0:o.props={internalLeftPadded:!0},o}function Me(e){return Array.isArray(e.dynamicProps)?e.dynamicProps.includes("internalLeftPadded")||e.dynamicProps.push("internalLeftPadded"):e.dynamicProps=["internalLeftPadded"],e}const mr={class:"breadcrumb","aria-label":"Breadcrumb"},xr={class:"muted"},yr={class:"page-head"},Cr={class:"page-head__title"},wr={class:"mono"},_r={class:"mono"},Sr={class:"mono"},Rr={class:"mono"},zr={style:{"margin-top":"12px"}},kr={class:"mono"},$r={class:"mono"},Tr={class:"mono"},Pr=le({__name:"ServerDetailPage",setup(e){const o=Ra(),c=Sa(),v=ka(),u=ja(),$=re(()=>String(o.params.id??"")),n=M(null),g=M(!1),m=M(null),_=M(!1),D=M(!1),y=M("overview"),j=Da("(max-width: 640px)"),H=re(()=>j.value?1:2),W=re(()=>{const s=(n.value?.name??"").replace(/[^A-Za-z0-9]/g,"");return s.length>=2?s.slice(0,2).toUpperCase():s.length===1?s.toUpperCase():"ND"}),B=re(()=>n.value?[`${n.value.ip}:${n.value.port}`,n.value.os??"Unknown OS",n.value.arch??"Unknown arch",n.value.docker_version??"Docker unknown"].join(" · "):"");function h(S){return S??"—"}function P(S){return S==null?"—":`${Oa(S)}%`}async function Z(){if(!$.value){m.value="Unknown server.";return}g.value=!0,m.value=null;try{n.value=await Ia($.value)}catch(S){n.value=null,m.value=De(S)}finally{g.value=!1}}async function G(){if(n.value){_.value=!0;try{const S=await u.validate(n.value.id);if(S.server&&(n.value=S.server),S.ok){v.success(`${n.value.name}: validation passed`);return}const s=S.checks.filter(z=>!z.ok).map(z=>z.name).join(", ");v.error(S.message||`${n.value.name}: failed checks: ${s}`)}catch(S){v.error(De(S))}finally{_.value=!1}}}async function X(){if(!n.value)return;const S=n.value.name;D.value=!0;try{await u.removeServer(n.value.id),v.success(`Deleted ${S}`),await c.push({name:"servers"})}catch(s){v.error(De(s))}finally{D.value=!1}}return ue($,()=>{y.value="overview",Z()}),vt(()=>{Z()}),(S,s)=>(i(),K(l(Se),{vertical:"",size:16},{default:f(()=>[L("nav",mr,[b(l(Oe),{to:"/servers"},{default:f(()=>[...s[1]||(s[1]=[I("Servers",-1)])]),_:1}),s[2]||(s[2]=L("span",{class:"breadcrumb__sep"},"/",-1)),L("span",xr,T(n.value?.name??$.value),1)]),b(l(Pa),{show:g.value},{default:f(()=>[m.value?(i(),K(l(ea),{key:0,type:"error","show-icon":!0,style:{"margin-bottom":"12px"}},{default:f(()=>[I(T(m.value),1)]),_:1})):et("",!0),n.value?(i(),x(ee,{key:1},[L("div",yr,[b(l(ta),{round:"",size:48},{default:f(()=>[I(T(W.value),1)]),_:1}),L("div",Cr,[b(l(Se),{align:"center",size:10},{default:f(()=>[b(l(Re),{strong:"",style:{"font-size":"20px"}},{default:f(()=>[I(T(n.value.name),1)]),_:1}),b(Wa,{status:n.value.status,size:"medium"},null,8,["status"])]),_:1}),b(l(Re),{depth:"3"},{default:f(()=>[I(T(B.value),1)]),_:1})]),b(l(Se),{class:"page-head__actions",align:"center",size:8},{default:f(()=>[b(l(he),{loading:_.value,onClick:G},{default:f(()=>[...s[3]||(s[3]=[I(" Validate ",-1)])]),_:1},8,["loading"]),b(l(Oe),{to:{name:"server-containers",params:{id:n.value.id}},custom:""},{default:f(({navigate:z})=>[b(l(he),{type:"primary",onClick:z},{default:f(()=>[...s[4]||(s[4]=[I(" Open containers ",-1)])]),_:1},8,["onClick"])]),_:1},8,["to"])]),_:1})]),b(l(gr),{value:y.value,"onUpdate:value":s[0]||(s[0]=z=>y.value=z),type:"line",animated:""},{default:f(()=>[b(l(ve),{name:"overview",tab:"Overview"},{default:f(()=>[b(l(Se),{vertical:"",size:16,style:{"margin-top":"16px"}},{default:f(()=>[b(l(de),{title:"Node info"},{default:f(()=>[b(l(ot),{column:H.value,bordered:"","label-placement":"left"},{default:f(()=>[b(l(V),{label:"Name"},{default:f(()=>[I(T(n.value.name),1)]),_:1}),b(l(V),{label:"Address"},{default:f(()=>[L("span",wr,T(n.value.ip)+":"+T(n.value.port),1)]),_:1}),b(l(V),{label:"SSH user"},{default:f(()=>[L("span",_r,T(n.value.ssh_user),1)]),_:1}),b(l(V),{label:"Node ID"},{default:f(()=>[L("span",Sr,T(h(n.value.node_id)),1)]),_:1}),b(l(V),{label:"OS"},{default:f(()=>[I(T(h(n.value.os)),1)]),_:1}),b(l(V),{label:"Architecture"},{default:f(()=>[I(T(h(n.value.arch)),1)]),_:1}),b(l(V),{label:"Docker"},{default:f(()=>[I(T(h(n.value.docker_version)),1)]),_:1}),b(l(V),{label:"SSH key"},{default:f(()=>[L("span",Rr,T(h(n.value.ssh_key_id)),1)]),_:1}),b(l(V),{label:"CPU usage"},{default:f(()=>[I(T(P(n.value.cpu_usage)),1)]),_:1}),b(l(V),{label:"Memory usage"},{default:f(()=>[I(T(P(n.value.mem_usage)),1)]),_:1}),b(l(V),{label:"Disk usage"},{default:f(()=>[I(T(P(n.value.disk_usage)),1)]),_:1}),b(l(V),{label:"Containers"},{default:f(()=>[I(T(n.value.container_count??"—"),1)]),_:1}),b(l(V),{label:"Last seen"},{default:f(()=>[I(T(l(ze)(n.value.last_seen)),1)]),_:1}),b(l(V),{label:"Registered"},{default:f(()=>[I(T(l(ze)(n.value.created_at)),1)]),_:1})]),_:1},8,["column"])]),_:1}),b(l(de),{title:"Labels"},{default:f(()=>[b(l(Ce),{description:"No labels on this node yet."})]),_:1}),b(l(de),{title:"Danger zone"},{default:f(()=>[b(l(Re),{depth:"3"},{default:f(()=>[...s[5]||(s[5]=[I(" Deleting a node removes it from the control plane only. Containers, volumes and certificates on the machine are kept. ",-1)])]),_:1}),L("div",zr,[b(l(Ta),{"positive-button-props":{type:"error"},onPositiveClick:X},{trigger:f(()=>[b(l(he),{type:"error",ghost:"",loading:D.value},{default:f(()=>[...s[6]||(s[6]=[I(" Delete node ",-1)])]),_:1},8,["loading"])]),default:f(()=>[I(' Delete server "'+T(n.value.name)+'"? ',1)]),_:1})])]),_:1})]),_:1})]),_:1}),b(l(ve),{name:"containers",tab:"Containers"},{default:f(()=>[b(l(de),{style:{"margin-top":"16px"}},{default:f(()=>[b(l(Ce),{description:"Container management lives on the containers page."},{extra:f(()=>[b(l(Oe),{to:{name:"server-containers",params:{id:n.value.id}},custom:""},{default:f(({navigate:z})=>[b(l(he),{type:"primary",onClick:z},{default:f(()=>[...s[7]||(s[7]=[I(" Open containers ",-1)])]),_:1},8,["onClick"])]),_:1},8,["to"])]),_:1})]),_:1})]),_:1}),b(l(ve),{name:"metrics",tab:"Metrics"},{default:f(()=>[b(l(de),{style:{"margin-top":"16px"}},{default:f(()=>[b(l(Ce),{description:"Metrics ship in Phase 8."})]),_:1})]),_:1}),b(l(ve),{name:"proxy",tab:"Proxy & Traefik"},{default:f(()=>[b(l(de),{style:{"margin-top":"16px"}},{default:f(()=>[b(l(Ce),{description:"Proxy & Traefik ships in Phase 6."})]),_:1})]),_:1}),b(l(ve),{name:"settings",tab:"Node settings"},{default:f(()=>[b(l(de),{title:"Node settings",style:{"margin-top":"16px"}},{default:f(()=>[b(l(Re),{depth:"3",style:{display:"block","margin-bottom":"12px"}},{default:f(()=>[...s[8]||(s[8]=[I(" Editable settings do not exist in the backend yet. Values below are read-only. ",-1)])]),_:1}),b(l(ot),{column:H.value,bordered:"","label-placement":"left"},{default:f(()=>[b(l(V),{label:"Name"},{default:f(()=>[I(T(n.value.name),1)]),_:1}),b(l(V),{label:"Address"},{default:f(()=>[L("span",kr,T(n.value.ip)+":"+T(n.value.port),1)]),_:1}),b(l(V),{label:"SSH user"},{default:f(()=>[L("span",$r,T(n.value.ssh_user),1)]),_:1}),b(l(V),{label:"SSH key"},{default:f(()=>[L("span",Tr,T(h(n.value.ssh_key_id)),1)]),_:1}),b(l(V),{label:"Registered"},{default:f(()=>[I(T(l(ze)(n.value.created_at)),1)]),_:1}),b(l(V),{label:"Updated"},{default:f(()=>[I(T(l(ze)(n.value.updated_at)),1)]),_:1})]),_:1},8,["column"])]),_:1})]),_:1})]),_:1},8,["value"])],64)):et("",!0)]),_:1},8,["show"])]),_:1}))}}),Kr=Ha(Pr,[["__scopeId","data-v-f0bf6150"]]);export{Kr as default};
