import{E as _e}from"./Empty-DGb97BFc.js";import{A as ea}from"./Alert-Dk2IJemi.js";import{A as ta}from"./Avatar-Djesj-8b.js";import{az as aa,aA as $e,aB as ra,d as le,ao as na,aC as oa,q as N,I as sa,a as E,N as k,O as a,ae as ct,a7 as u,ac as X,aD as la,aE as ia,P as Pe,af as ke,o as l,c as m,G as y,a1 as te,E as R,aF as da,Q as bt,aa as ft,x as re,aG as ca,ab as ae,ah as ba,ak as ut,al as fa,aw as Te,F as ee,j as q,aH as ua,aI as pa,aJ as va,_ as pt,B as he,H as Qe,aK as Ie,U as ha,y as ue,g as vt,aL as ga,aM as ma,aN as xa,a8 as Ze,W as pe,aO as Ce,aP as ya,aQ as _a,aR as Ca,ai as wa,a0 as se,V as we,w as b,u as s,e as c,k as D,p as Oe,t as B,l as et,C as de,i as Sa,h as Ra}from"./index-Dd6Mwd7g.js";import{g as za,u as Ne,S as Se,t as Re}from"./text-Bpe7jiRD.js";import{u as ka}from"./use-message-t-bVr5Da.js";import{A as $a,_ as Ta,r as ze,P as Pa}from"./format-B88qN660.js";import{S as Ba}from"./Spin-CyDRFFPd.js";import{a as Aa,b as tt,C as La,o as Ea,g as Wa,d as De}from"./servers-DDbxX1d5.js";import{u as Ia}from"./format-length-Ls04XJrq.js";import{u as Oa}from"./servers-CIDvAnI1.js";import{_ as Da}from"./_plugin-vue_export-helper-DlAUqK2U.js";import"./use-locale-Dqpb18Yk.js";var ja=/\s/;function Ha(e){for(var o=e.length;o--&&ja.test(e.charAt(o)););return o}var Ma=/^\s+/;function Na(e){return e&&e.slice(0,Ha(e)+1).replace(Ma,"")}var at=NaN,Fa=/^[-+]0x[0-9a-f]+$/i,Va=/^0b[01]+$/i,Ua=/^0o[0-7]+$/i,Ga=parseInt;function rt(e){if(typeof e=="number")return e;if(aa(e))return at;if($e(e)){var o=typeof e.valueOf=="function"?e.valueOf():e;e=$e(o)?o+"":o}if(typeof e!="string")return e===0?e:+e;e=Na(e);var d=Va.test(e);return d||Ua.test(e)?Ga(e.slice(2),d?2:8):Fa.test(e)?at:+e}var je=function(){return ra.Date.now()},Xa="Expected a function",qa=Math.max,Ka=Math.min;function Ya(e,o,d){var p,f,$,n,h,g,w=0,H=!1,x=!1,M=!0;if(typeof e!="function")throw new TypeError(Xa);o=rt(o)||0,$e(d)&&(H=!!d.leading,x="maxWait"in d,$=x?qa(rt(d.maxWait)||0,o):$,M="trailing"in d?!!d.trailing:M);function F(z){var I=p,Y=f;return p=f=void 0,w=z,n=e.apply(Y,I),n}function T(z){return w=z,h=setTimeout(W,o),H?F(z):n}function P(z){var I=z-g,Y=z-w,V=o-I;return x?Ka(V,$-Y):V}function _(z){var I=z-g,Y=z-w;return g===void 0||I>=o||I<0||x&&Y>=$}function W(){var z=je();if(_(z))return Z(z);h=setTimeout(W,P(z))}function Z(z){return h=void 0,M&&p?F(z):(p=f=void 0,n)}function S(){h!==void 0&&clearTimeout(h),w=0,p=g=f=h=void 0}function v(){return h===void 0?n:Z(je())}function A(){var z=je(),I=_(z);if(p=arguments,f=this,g=z,I){if(h===void 0)return T(g);if(x)return clearTimeout(h),h=setTimeout(W,o),F(g)}return h===void 0&&(h=setTimeout(W,o)),n}return A.cancel=S,A.flush=v,A}var Ja="Expected a function";function Qa(e,o,d){var p=!0,f=!0;if(typeof e!="function")throw new TypeError(Ja);return $e(d)&&(p="leading"in d?!!d.leading:p,f="trailing"in d?!!d.trailing:f),Ya(e,o,{leading:p,maxWait:o,trailing:f})}const Za=tt(".v-x-scroll",{overflow:"auto",scrollbarWidth:"none"},[tt("&::-webkit-scrollbar",{width:0,height:0})]),er=le({name:"XScroll",props:{disabled:Boolean,onScroll:Function},setup(){const e=N(null);function o(f){!(f.currentTarget.offsetWidth<f.currentTarget.scrollWidth)||f.deltaY===0||(f.currentTarget.scrollLeft+=f.deltaY+f.deltaX,f.preventDefault())}const d=oa();return Za.mount({id:"vueuc/x-scroll",head:!0,anchorMetaName:Aa,ssr:d}),Object.assign({selfRef:e,handleWheel:o},{scrollTo(...f){var $;($=e.value)===null||$===void 0||$.scrollTo(...f)}})},render(){return na("div",{ref:"selfRef",onScroll:this.onScroll,onWheel:this.disabled?void 0:this.handleWheel,class:"v-x-scroll"},this.$slots)}});var tr=le({name:"ChevronLeft",render(){return(()=>{const e=sa("dfe229c2639b2082");return e[0]||(e[0]=E("svg",{viewBox:"0 0 16 16",fill:"none",xmlns:"http://www.w3.org/2000/svg"},[E("path",{d:"M10.3536 3.14645C10.5488 3.34171 10.5488 3.65829 10.3536 3.85355L6.20711 8L10.3536 12.1464C10.5488 12.3417 10.5488 12.6583 10.3536 12.8536C10.1583 13.0488 9.84171 13.0488 9.64645 12.8536L5.14645 8.35355C4.95118 8.15829 4.95118 7.84171 5.14645 7.64645L9.64645 3.14645C9.84171 2.95118 10.1583 2.95118 10.3536 3.14645Z",fill:"currentColor"})],-1))})()}});function nt(e,o="default",d=[]){const{children:p}=e;if(p!==null&&typeof p=="object"&&!Array.isArray(p)){const f=p[o];if(typeof f=="function")return f()}return d}var ar=k([a("descriptions",{fontSize:"var(--n-font-size)"},[a("descriptions-separator",`
 display: inline-block;
 margin: 0 8px 0 2px;
 `),a("descriptions-table-wrapper",[a("descriptions-table",[a("descriptions-table-row",[a("descriptions-table-header",{padding:"var(--n-th-padding)"}),a("descriptions-table-content",{padding:"var(--n-td-padding)"})])])]),ct("bordered",[a("descriptions-table-wrapper",[a("descriptions-table",[a("descriptions-table-row",[k("&:last-child",[a("descriptions-table-content",{paddingBottom:0})])])])])]),u("left-label-placement",[a("descriptions-table-content",[k("> *",{verticalAlign:"top"})])]),u("left-label-align",[k("th",{textAlign:"left"})]),u("center-label-align",[k("th",{textAlign:"center"})]),u("right-label-align",[k("th",{textAlign:"right"})]),u("bordered",[a("descriptions-table-wrapper",`
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
 `,[X("content",`
 transition: color .3s var(--n-bezier);
 display: inline-block;
 color: var(--n-td-text-color);
 `)]),X("label",`
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
 `))]);const rr="DESCRIPTION_ITEM_FLAG";function nr(e){return typeof e=="object"&&e&&!Array.isArray(e)?e.type&&e.type.DESCRIPTION_ITEM_FLAG:!1}const or=["colspan"],sr=["colspan"],lr=["colspan"],ir=["colspan"],dr={...Pe.props,title:String,column:{type:Number,default:3},columns:Number,labelPlacement:{type:String,default:"top"},labelAlign:{type:String,default:"left"},separator:{type:String,default:":"},size:String,bordered:Boolean,labelClass:String,labelStyle:[Object,String],contentClass:String,contentStyle:[Object,String]};var ot=le({name:"Descriptions",props:dr,slots:Object,setup(e){const{mergedClsPrefixRef:o,inlineThemeDisabled:d,mergedComponentPropsRef:p}=bt(e),f=re(()=>e.size||p?.value?.Descriptions?.size||"medium"),$=Pe("Descriptions","-descriptions",ar,ca,e,o),n=re(()=>{const{bordered:g}=e,w=f.value,{common:{cubicBezierEaseInOut:H},self:{titleTextColor:x,thColor:M,thColorModal:F,thColorPopover:T,thTextColor:P,thFontWeight:_,tdTextColor:W,tdColor:Z,tdColorModal:S,tdColorPopover:v,borderColor:A,borderColorModal:z,borderColorPopover:I,borderRadius:Y,lineHeight:V,[ae("fontSize",w)]:ne,[ae(g?"thPaddingBordered":"thPadding",w)]:Q,[ae(g?"tdPaddingBordered":"tdPadding",w)]:L}}=$.value;return{"--n-title-text-color":x,"--n-th-padding":Q,"--n-td-padding":L,"--n-font-size":ne,"--n-bezier":H,"--n-th-font-weight":_,"--n-line-height":V,"--n-th-text-color":P,"--n-td-text-color":W,"--n-th-color":M,"--n-th-color-modal":F,"--n-th-color-popover":T,"--n-td-color":Z,"--n-td-color-modal":S,"--n-td-color-popover":v,"--n-border-radius":Y,"--n-border-color":A,"--n-border-color-modal":z,"--n-border-color-popover":I}}),h=d?ft("descriptions",re(()=>{let g="";const{bordered:w}=e;return w&&(g+="a"),g+=f.value[0],g}),n,e):void 0;return{mergedClsPrefix:o,cssVars:d?void 0:n,themeClass:h?.themeClass,onRender:h?.onRender,compitableColumn:Ne(e,["columns","column"]),inlineThemeDisabled:d,mergedSize:f}},render(){const e=this.$slots.default,o=e?ke(e()):[];o.length;const{contentClass:d,labelClass:p,compitableColumn:f,labelPlacement:$,labelAlign:n,mergedSize:h,bordered:g,title:w,cssVars:H,mergedClsPrefix:x,separator:M,onRender:F}=this;F?.();const T=o.filter(_=>nr(_)),P=T.reduce((_,W,Z)=>{const S=W.props||{},v=T.length-1===Z,A=["label"in S?S.label:nt(W,"label")],z=[nt(W)],I=S.span||1,Y=_.span;_.span+=I;const V=S.labelStyle||S["label-style"]||this.labelStyle,ne=S.contentStyle||S["content-style"]||this.contentStyle;if($==="left")g?_.row.push((l(),m("th",{key:1,class:R([`${x}-descriptions-table-header`,p]),colspan:1,style:te(V)},[y(()=>A)],6)),(l(),m("td",{key:2,class:R([`${x}-descriptions-table-content`,d]),colspan:v?(f-Y)*2+1:I*2-1,style:te(ne)},[y(()=>z)],14,or))):_.row.push((l(),m("td",{key:3,class:R(`${x}-descriptions-table-content`),colspan:v?(f-Y)*2:I*2},[E("span",{class:R([`${x}-descriptions-table-content__label`,p]),style:te(V)},[y(()=>[...A,M&&(l(),m("span",{key:4,class:R(`${x}-descriptions-separator`)},[y(()=>M)],2))])],6),E("span",{class:R([`${x}-descriptions-table-content__content`,d]),style:te(ne)},[y(()=>z)],6)],10,sr)));else{const Q=v?(f-Y)*2:I*2;_.row.push((l(),m("th",{key:5,class:R([`${x}-descriptions-table-header`,p]),colspan:Q,style:te(V)},[y(()=>A)],14,lr))),_.secondRow.push((l(),m("td",{key:6,class:R([`${x}-descriptions-table-content`,d]),colspan:Q,style:te(ne)},[y(()=>z)],14,ir)))}return(_.span>=f||v)&&(_.span=0,_.row.length&&(_.rows.push(_.row),_.row=[]),$!=="left"&&_.secondRow.length&&(_.rows.push(_.secondRow),_.secondRow=[])),_},{span:0,row:[],secondRow:[],rows:[]}).rows.map(_=>(l(),m("tr",{class:R(`${x}-descriptions-table-row`)},[y(()=>_)],2)));return l(),m("div",{style:te(H),class:R([`${x}-descriptions`,this.themeClass,`${x}-descriptions--${$}-label-placement`,`${x}-descriptions--${n}-label-align`,`${x}-descriptions--${h}-size`,g&&`${x}-descriptions--bordered`])},[w||this.$slots.header?(l(),m("div",{key:0,class:R(`${x}-descriptions-header`)},[y(()=>w||za(this,"header"))],2)):y(()=>null),E("div",{class:R(`${x}-descriptions-table-wrapper`)},[E("table",{class:R(`${x}-descriptions-table`)},[E("tbody",null,[y(()=>$==="top"&&(l(),m("tr",{class:R(`${x}-descriptions-table-row`),style:{visibility:"collapse"}},[y(()=>da(f*2,(l(),m("td"))))],2))),y(()=>P)])],2)],2)],6)}});const cr={label:String,span:{type:Number,default:1},labelClass:String,labelStyle:[Object,String],contentClass:String,contentStyle:[Object,String]};var G=le({name:"DescriptionsItem",[rr]:!0,props:cr,slots:Object,render(){return null}});const Ve=ba("n-tabs"),ht={tab:[String,Number,Object,Function],name:{type:[String,Number],required:!0},disabled:Boolean,displayDirective:{type:String,default:"if"},closable:{type:Boolean,default:void 0},tabProps:Object,label:[String,Number,Object,Function]};var ve=le({__TAB_PANE__:!0,name:"TabPane",alias:["TabPanel"],props:ht,slots:Object,setup(e){const o=ut(Ve,null);return o||fa("tab-pane","`n-tab-pane` must be placed inside `n-tabs`."),{style:o.paneStyleRef,class:o.paneClassRef,mergedClsPrefix:o.mergedClsPrefixRef}},render(){return l(),m("div",{class:R([`${this.mergedClsPrefix}-tab-pane`,this.class]),style:te(this.style)},[y(()=>this.$slots.default?.())],6)}});const br=["data-name","data-disabled"],fr={internalLeftPadded:Boolean,internalAddable:Boolean,internalCreatedByPane:Boolean,...va(ht,["displayDirective"])};var Fe=le({__TAB__:!0,inheritAttrs:!1,name:"Tab",props:fr,setup(e){const{mergedClsPrefixRef:o,valueRef:d,typeRef:p,closableRef:f,tabStyleRef:$,addTabStyleRef:n,tabClassRef:h,addTabClassRef:g,tabChangeIdRef:w,onBeforeLeaveRef:H,triggerRef:x,handleAdd:M,activateTab:F,handleClose:T}=ut(Ve);return{trigger:x,mergedClosable:re(()=>{if(e.internalAddable)return!1;const{closable:P}=e;return P===void 0?f.value:P}),style:$,addStyle:n,tabClass:h,addTabClass:g,clsPrefix:o,value:d,type:p,handleClose(P){P.stopPropagation(),!e.disabled&&T(e.name)},activateTab(){if(e.disabled)return;if(e.internalAddable){M();return}const{name:P}=e,_=++w.id;if(P!==d.value){const{value:W}=H;W?Promise.resolve(W(e.name,d.value)).then(Z=>{Z&&w.id===_&&F(P)}):F(P)}}}},render(){const{internalAddable:e,clsPrefix:o,name:d,disabled:p,label:f,tab:$,value:n,mergedClosable:h,trigger:g,$slots:{default:w}}=this,H=f??$;return l(),m("div",{class:R(`${o}-tabs-tab-wrapper`)},[this.internalLeftPadded?(l(),m("div",{key:0,class:R(`${o}-tabs-tab-pad`)},null,2)):y(()=>null),(l(),m("div",Te({key:d,"data-name":d,"data-disabled":p?!0:void 0},Te({class:[`${o}-tabs-tab`,n===d&&`${o}-tabs-tab--active`,p&&`${o}-tabs-tab--disabled`,h&&`${o}-tabs-tab--closable`,e&&`${o}-tabs-tab--addable`,e?this.addTabClass:this.tabClass],onClick:g==="click"?this.activateTab:void 0,onMouseenter:g==="hover"?this.activateTab:void 0,style:e?this.addStyle:this.style},this.internalCreatedByPane?this.tabProps||{}:this.$attrs)),[E("span",{class:R(`${o}-tabs-tab__label`)},[e?(l(),m(ee,{key:0},[E("div",{class:R(`${o}-tabs-tab__height-placeholder`)}," ",2),(l(),q(pt,{clsPrefix:o},{default:()=>(l(),q($a))},1032,["clsPrefix"]))],64)):(l(),m(ee,{key:1},[w?(l(),m(ee,{key:0},[y(()=>w())],64)):(l(),m(ee,{key:1},[typeof H=="object"?(l(),m(ee,{key:0},[y(()=>H)],64)):(l(),m(ee,{key:1},[y(()=>ua(H??d))],64))],64))],64))],2),h&&this.type==="card"?(l(),q(pa,{key:0,clsPrefix:o,class:R(`${o}-tabs-tab__close`),onClick:this.handleClose,disabled:p},null,8,["clsPrefix","class","onClick","disabled"])):y(()=>null)],16,br))],2)}}),ur=a("tabs",`
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
 `)]),u("segment-type",[a("tabs-rail",[k("&.transition-disabled",[a("tabs-capsule",`
 transition: none;
 `)])])]),u("top",[a("tab-pane",`
 padding: var(--n-pane-padding-top) var(--n-pane-padding-right) var(--n-pane-padding-bottom) var(--n-pane-padding-left);
 `)]),u("left",[a("tab-pane",`
 padding: var(--n-pane-padding-right) var(--n-pane-padding-bottom) var(--n-pane-padding-left) var(--n-pane-padding-top);
 `)]),u("left, right",`
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
 `)]),u("right",`
 flex-direction: row-reverse;
 `,[a("tab-pane",`
 padding: var(--n-pane-padding-left) var(--n-pane-padding-top) var(--n-pane-padding-right) var(--n-pane-padding-bottom);
 `),a("tabs-bar",`
 left: 0;
 `)]),u("bottom",`
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
 `,[u("active",`
 font-weight: var(--n-font-weight-strong);
 color: var(--n-tab-text-color-active);
 `),k("&:hover",`
 color: var(--n-tab-text-color-hover);
 `)])])]),u("flex",[a("tabs-nav",`
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
 `,[X("prefix, suffix",`
 display: flex;
 align-items: center;
 `),X("prefix","padding-right: 16px;"),X("suffix","padding-left: 16px;")]),u("top, bottom",[k(">",[a("tabs-nav",[a("tabs-nav-scroll-wrapper",[k("&::before",`
 top: 0;
 bottom: 0;
 left: 0;
 width: 20px;
 `),k("&::after",`
 top: 0;
 bottom: 0;
 right: 0;
 width: 20px;
 `),u("shadow-start",[k("&::before",`
 box-shadow: inset 10px 0 8px -8px rgba(0, 0, 0, .12);
 `)]),u("shadow-end",[k("&::after",`
 box-shadow: inset -10px 0 8px -8px rgba(0, 0, 0, .12);
 `)])])])])]),u("left, right",[a("tabs-nav-scroll-content",`
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
 `),u("shadow-start",[k("&::before",`
 box-shadow: inset 0 10px 8px -8px rgba(0, 0, 0, .12);
 `)]),u("shadow-end",[k("&::after",`
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
 `,[u("disabled",{cursor:"not-allowed"}),X("close",`
 margin-inline-start: 6px;
 transition:
 background-color .3s var(--n-bezier),
 color .3s var(--n-bezier);
 `),X("label",`
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
 `),u("disabled",`
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
 `),u("line-type, bar-type",[a("tabs-tab",`
 font-weight: var(--n-tab-font-weight);
 box-sizing: border-box;
 vertical-align: bottom;
 `,[k("&:hover",{color:"var(--n-tab-text-color-hover)"}),u("active",`
 color: var(--n-tab-text-color-active);
 font-weight: var(--n-tab-font-weight-active);
 `),u("disabled",{color:"var(--n-tab-text-color-disabled)"})])]),a("tabs-nav",[X("prefix, suffix",`
 border-color: var(--n-tab-border-color);
 `),a("tabs-nav-scroll-content",`
 border-color: var(--n-tab-border-color);
 `),u("line-type",[u("top",[X("prefix, suffix",`
 border-bottom: 1px solid var(--n-tab-border-color);
 `),a("tabs-nav-scroll-content",`
 border-bottom: 1px solid var(--n-tab-border-color);
 `),a("tabs-bar",`
 bottom: -1px;
 `)]),u("left",[X("prefix, suffix",`
 border-right: 1px solid var(--n-tab-border-color);
 `),a("tabs-nav-scroll-content",`
 border-right: 1px solid var(--n-tab-border-color);
 `),a("tabs-bar",`
 right: -1px;
 `)]),u("right",[X("prefix, suffix",`
 border-left: 1px solid var(--n-tab-border-color);
 `),a("tabs-nav-scroll-content",`
 border-left: 1px solid var(--n-tab-border-color);
 `),a("tabs-bar",`
 left: -1px;
 `)]),u("bottom",[X("prefix, suffix",`
 border-top: 1px solid var(--n-tab-border-color);
 `),a("tabs-nav-scroll-content",`
 border-top: 1px solid var(--n-tab-border-color);
 `),a("tabs-bar",`
 top: -1px;
 `)]),X("prefix, suffix",`
 transition: border-color .3s var(--n-bezier);
 `),a("tabs-nav-scroll-content",`
 transition: border-color .3s var(--n-bezier);
 `),a("tabs-bar",`
 border-radius: 0;
 `)]),u("card-type",[X("prefix, suffix",`
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
 `,[u("addable",`
 padding-left: 8px;
 padding-right: 8px;
 font-size: 16px;
 justify-content: center;
 `,[X("height-placeholder",`
 width: 0;
 font-size: var(--n-tab-font-size);
 `),ct("disabled",[k("&:hover",`
 color: var(--n-tab-text-color-hover);
 `)])]),u("closable","padding-inline-end: 8px;"),u("active",`
 background-color: #0000;
 font-weight: var(--n-tab-font-weight-active);
 color: var(--n-tab-text-color-active);
 `),u("disabled","color: var(--n-tab-text-color-disabled);")])]),u("left, right",`
 flex-direction: column; 
 `,[X("prefix, suffix",`
 padding: var(--n-tab-padding-vertical);
 `),a("tabs-wrapper",`
 flex-direction: column;
 `),a("tabs-tab-wrapper",`
 flex-direction: column;
 `,[a("tabs-tab-pad",`
 height: var(--n-tab-gap-vertical);
 width: 100%;
 `)])]),u("top",[u("card-type",[a("tabs-scroll-padding","border-bottom: 1px solid var(--n-tab-border-color);"),X("prefix, suffix",`
 border-bottom: 1px solid var(--n-tab-border-color);
 `),a("tabs-tab",`
 border-top-left-radius: var(--n-tab-border-radius);
 border-top-right-radius: var(--n-tab-border-radius);
 `,[u("active",`
 border-bottom: 1px solid #0000;
 `)]),a("tabs-tab-pad",`
 border-bottom: 1px solid var(--n-tab-border-color);
 `),a("tabs-pad",`
 border-bottom: 1px solid var(--n-tab-border-color);
 `)])]),u("left",[u("card-type",[a("tabs-scroll-padding","border-right: 1px solid var(--n-tab-border-color);"),X("prefix, suffix",`
 border-right: 1px solid var(--n-tab-border-color);
 `),a("tabs-tab",`
 border-top-left-radius: var(--n-tab-border-radius);
 border-bottom-left-radius: var(--n-tab-border-radius);
 `,[u("active",`
 border-right: 1px solid #0000;
 `)]),a("tabs-tab-pad",`
 border-right: 1px solid var(--n-tab-border-color);
 `),a("tabs-pad",`
 border-right: 1px solid var(--n-tab-border-color);
 `)])]),u("right",[u("card-type",[a("tabs-scroll-padding","border-left: 1px solid var(--n-tab-border-color);"),X("prefix, suffix",`
 border-left: 1px solid var(--n-tab-border-color);
 `),a("tabs-tab",`
 border-top-right-radius: var(--n-tab-border-radius);
 border-bottom-right-radius: var(--n-tab-border-radius);
 `,[u("active",`
 border-left: 1px solid #0000;
 `)]),a("tabs-tab-pad",`
 border-left: 1px solid var(--n-tab-border-color);
 `),a("tabs-pad",`
 border-left: 1px solid var(--n-tab-border-color);
 `)])]),u("bottom",[u("card-type",[a("tabs-scroll-padding","border-top: 1px solid var(--n-tab-border-color);"),X("prefix, suffix",`
 border-top: 1px solid var(--n-tab-border-color);
 `),a("tabs-tab",`
 border-bottom-left-radius: var(--n-tab-border-radius);
 border-bottom-right-radius: var(--n-tab-border-radius);
 `,[u("active",`
 border-top: 1px solid #0000;
 `)]),a("tabs-tab-pad",`
 border-top: 1px solid var(--n-tab-border-color);
 `),a("tabs-pad",`
 border-top: 1px solid var(--n-tab-border-color);
 `)])])]),a("tabs-scroll-button",[u("start",`
 padding-left: 10px;
 padding-right: 6px;
 `),u("end",`
 padding-right: 10px;
 padding-left: 6px;
 `),u("up",`
 padding-bottom: 10px;
 `),u("down",`
 padding-top: 10px;
 `)])]),st=le({name:"TabsButton",props:{type:{type:String,default:"next"},mergedClsPrefix:{type:String,required:!0},vertical:Boolean,disabled:Boolean,rtl:Boolean,theme:Object,themeOverrides:Object,onClick:Function},setup(e){return{handleClick:()=>{e.disabled||e.onClick?.(e.type)}}},render(){const{mergedClsPrefix:e,disabled:o,type:d,vertical:p,rtl:f,theme:$,themeOverrides:n,handleClick:h}=this,g=d==="next",w=p?g:f?!g:g;return l(),q(he,{text:!0,disabled:o,size:"small",theme:$,themeOverrides:n,onClick:h,class:R([`${e}-tabs-scroll-button`,!p&&d==="prev"&&`${e}-tabs-scroll-button--start`,!p&&d==="next"&&`${e}-tabs-scroll-button--end`,p&&d==="prev"&&`${e}-tabs-scroll-button--up`,p&&d==="next"&&`${e}-tabs-scroll-button--down`])},{icon:()=>(l(),q(pt,{clsPrefix:e,style:te(p?{transform:"rotate(90deg)"}:void 0)},{default:()=>w?(l(),q(La,{key:1})):(l(),q(tr,{key:2}))},1032,["clsPrefix","style"]))},1032,["disabled","theme","themeOverrides","onClick","class"])}});const He=Qa,pr={...Pe.props,value:[String,Number],defaultValue:[String,Number],trigger:{type:String,default:"click"},type:{type:String,default:"bar"},closable:Boolean,justifyContent:String,size:String,placement:{type:String,default:"top"},tabStyle:[String,Object],tabClass:String,addTabStyle:[String,Object],addTabClass:String,barWidth:Number,paneClass:String,paneStyle:[String,Object],paneWrapperClass:String,paneWrapperStyle:[String,Object],addable:[Boolean,Object],tabsPadding:{type:Number,default:0},animated:Boolean,onBeforeLeave:Function,onAdd:Function,"onUpdate:value":[Function,Array],onUpdateValue:[Function,Array],onClose:[Function,Array],labelSize:String,activeName:[String,Number],onActiveNameChange:[Function,Array],showScrollButton:Boolean,centerActiveTab:Boolean};var vr=le({name:"Tabs",props:pr,slots:Object,setup(e,{slots:o}){const{mergedClsPrefixRef:d,inlineThemeDisabled:p,mergedComponentPropsRef:f,mergedRtlRef:$}=bt(e),n=ha("Tabs",$,d),h=re(()=>{const{placement:t}=e;return t==="start"?n?.value?"right":"left":t==="end"?n?.value?"left":"right":t}),g=Pe("Tabs","-tabs",ur,xa,e,d),w=N(null),H=N(null),x=N(null),M=N(null),F=N(null),T=N(null),P=N(null),_=N(!0),W=N(!0),Z=Ne(e,["labelSize","size"]),S=re(()=>{if(Z.value)return Z.value;const t=f?.value?.Tabs?.size;return t||"medium"}),v=Ne(e,["activeName","value"]),A=N(v.value??e.defaultValue??(o.default?ke(o.default())[0]?.props?.name:null)),z=Ia(v,A),I={id:0},Y=re(()=>{if(!(!e.justifyContent||e.type==="card"))return{display:"flex",justifyContent:e.justifyContent}});ue(z,()=>{I.id=0,J(),pe(()=>{Be()})});function V(){const{value:t}=z;return t===null?null:w.value?.querySelector(`[data-name="${t}"]`)}function ne(t){if(e.type==="card")return;const{value:r}=x;if(!r)return;const i=r.style.opacity==="0";if(t){const C=`${d.value}-tabs-bar--disabled`,{barWidth:O}=e,U=h.value;if(t.dataset.disabled==="true"?r.classList.add(C):r.classList.remove(C),["top","bottom"].includes(U)){if(L(["top","maxHeight","height"]),typeof O=="number"&&t.offsetWidth>=O){const j=Math.floor((t.offsetWidth-O)/2)+t.offsetLeft;r.style.left=`${j}px`,r.style.maxWidth=`${O}px`}else r.style.left=`${t.offsetLeft}px`,r.style.maxWidth=`${t.offsetWidth}px`;r.style.width="8192px",i&&(r.style.transition="none"),r.offsetWidth,i&&(r.style.transition="",r.style.opacity="1")}else{if(L(["left","maxWidth","width"]),typeof O=="number"&&t.offsetHeight>=O){const j=Math.floor((t.offsetHeight-O)/2)+t.offsetTop;r.style.top=`${j}px`,r.style.maxHeight=`${O}px`}else r.style.top=`${t.offsetTop}px`,r.style.maxHeight=`${t.offsetHeight}px`;r.style.height="8192px",i&&(r.style.transition="none"),r.offsetHeight,i&&(r.style.transition="",r.style.opacity="1")}}}function Q(){if(e.type==="card")return;const{value:t}=x;t&&(t.style.opacity="0")}function L(t){const{value:r}=x;if(r)for(const i of t)r.style[i]=""}function J(){if(e.type==="card")return;const t=V();t?ne(t):Q()}function ce(t,r,i,C){const O=t.getBoundingClientRect(),U=r.getBoundingClientRect(),j=i?"left":"top",K=i?"right":"bottom";let oe=0;C?oe=(U[j]+U[K])/2-(O[j]+O[K])/2:U[j]<O[j]?oe=U[j]-O[j]:U[K]>O[K]&&(oe=U[K]-O[K]),oe!==0&&t.scrollBy({[j]:oe,behavior:"smooth"})}function Be(){const t=["top","bottom"].includes(h.value),r=V();if(r)if(t){const i=T.value?.$el;if(!i)return;ce(i,r,t,e.centerActiveTab)}else{const{value:i}=P;if(!i)return;ce(i,r,t,e.centerActiveTab)}}const ge=N(null);let Ae=0,ie=null;function gt(t){const r=ge.value;if(r){Ae=t.getBoundingClientRect().height;const i=`${Ae}px`,C=()=>{r.style.height=i,r.style.maxHeight=i};ie?(C(),ie(),ie=null):ie=C}}function mt(t){const r=ge.value;if(r){const i=t.getBoundingClientRect().height,C=()=>{document.body.offsetHeight,r.style.maxHeight=`${i}px`,r.style.height=`${Math.max(Ae,i)}px`};ie?(ie(),ie=null,C()):ie=C}}function xt(){const t=ge.value;if(t){t.style.maxHeight="",t.style.height="";const{paneWrapperStyle:r}=e;if(typeof r=="string")t.style.cssText=r;else if(r){const{maxHeight:i,height:C}=r;i!==void 0&&(t.style.maxHeight=i),C!==void 0&&(t.style.height=C)}}}const Ue={value:[]},Ge=N("next");function yt(t){const r=z.value;let i="next";for(const C of Ue.value){if(C===r)break;if(C===t){i="prev";break}}Ge.value=i,_t(t)}function _t(t){const{onActiveNameChange:r,onUpdateValue:i,"onUpdate:value":C}=e;r&&we(r,t),i&&we(i,t),C&&we(C,t),A.value=t}function Ct(t){const{onClose:r}=e;r&&we(r,t)}function wt(t){if(["top","bottom"].includes(h.value)){const{value:r}=T;if(!r)return;const i=r.$el;if(!i)return;const C=i.offsetWidth,O=!!n?.value,U=t==="next"?C:-C;i.scrollBy({left:O?-U:U,behavior:"smooth"})}else{const{value:r}=P;if(!r)return;const i=r.offsetHeight,C=t==="next"?r.scrollTop+i:r.scrollTop-i;r.scrollTo({top:C,left:0,behavior:"smooth"})}}let Le=!0;function Ee(){const{value:t}=x;if(!t)return;Le&&(Le=!1);const r="transition-disabled";t.classList.add(r),J(),t.classList.remove(r)}const be=N(null);function me({transitionDisabled:t}){const r=w.value;if(!r)return;t&&r.classList.add("transition-disabled");const i=V();i&&be.value&&(be.value.style.width=`${i.offsetWidth}px`,be.value.style.height=`${i.offsetHeight}px`,be.value.style.transform=`translate(${i.offsetLeft}px, ${i.offsetTop}px)`,t&&be.value.offsetWidth),t&&r.classList.remove("transition-disabled")}ue([z],()=>{e.type==="segment"&&pe(()=>{me({transitionDisabled:!1})})}),vt(()=>{e.type==="segment"&&me({transitionDisabled:!0})});let Xe=0;function St(t){if(t.contentRect.width===0&&t.contentRect.height===0||Xe===t.contentRect.width)return;Xe=t.contentRect.width;const{type:r}=e;(r==="line"||r==="bar")&&(Le||e.justifyContent?.startsWith("space"))&&Ee(),r!=="segment"&&xe(Ke())}const Rt=He(St,64);function qe(){const{type:t}=e;t==="line"||t==="bar"?Ee():t==="segment"&&me({transitionDisabled:!0})}ue([()=>e.justifyContent,()=>e.size],()=>{pe(()=>{(e.type==="line"||e.type==="bar")&&Ee()})}),ue([h,()=>n?.value],()=>{pe(()=>{qe(),xe(Ke(),{instantly:!0})})}),ue(()=>e.type,()=>{pe(()=>{const t=H.value;t&&(t.classList.add("transition-disabled"),qe(),t.offsetWidth,t.classList.remove("transition-disabled"))})});const fe=N(!1);function zt(t){const{target:r,contentRect:{width:i,height:C}}=t,O=r.parentElement.parentElement.offsetWidth,U=r.parentElement.parentElement.offsetHeight,j=h.value;if(!fe.value)j==="top"||j==="bottom"?O<i&&(fe.value=!0):U<C&&(fe.value=!0);else{const{value:K}=F;if(!K)return;j==="top"||j==="bottom"?O-i>K.$el.offsetWidth&&(fe.value=!1):U-C>K.$el.offsetHeight&&(fe.value=!1)}xe(T.value?.$el||null)}const kt=He(zt,64);function $t(){const{onAdd:t}=e;t&&t()}const We=N(!1);function Ke(){const t=h.value;return(t==="top"||t==="bottom"?T.value?.$el:P.value)||null}function xe(t,r={instantly:!1}){if(!t)return;const i=r.instantly?M.value:null;i&&i.classList.add("transition-disabled");const C=1,O=h.value;if(O==="top"||O==="bottom"){const{scrollLeft:U,scrollWidth:j,offsetWidth:K}=t,oe=Math.abs(U);_.value=oe<=C,W.value=oe+K>=j-C,We.value=K<j-C}else{const{scrollTop:U,scrollHeight:j,offsetHeight:K}=t;_.value=U<=C,W.value=U+K>=j-C,We.value=K<j-C}i&&(i.offsetWidth,i.classList.remove("transition-disabled"))}const Tt=He(t=>{xe(t.target)},64);wa(Ve,{triggerRef:se(e,"trigger"),tabStyleRef:se(e,"tabStyle"),tabClassRef:se(e,"tabClass"),addTabStyleRef:se(e,"addTabStyle"),addTabClassRef:se(e,"addTabClass"),paneClassRef:se(e,"paneClass"),paneStyleRef:se(e,"paneStyle"),mergedClsPrefixRef:d,typeRef:se(e,"type"),closableRef:se(e,"closable"),valueRef:z,tabChangeIdRef:I,onBeforeLeaveRef:se(e,"onBeforeLeave"),activateTab:yt,handleClose:Ct,handleAdd:$t}),Ea(()=>{J(),Be()}),ga(()=>{const{value:t}=M;if(!t)return;const{value:r}=d,i=`${r}-tabs-nav-scroll-wrapper--shadow-start`,C=`${r}-tabs-nav-scroll-wrapper--shadow-end`;_.value?t.classList.remove(i):t.classList.add(i),W.value?t.classList.remove(C):t.classList.add(C)});const Pt={syncBarPosition:()=>{J()},scrollToCurrentTab:()=>{Be()}},Bt=()=>{me({transitionDisabled:!0})},Ye=re(()=>{const{value:t}=S,{type:r}=e,i=`${t}${{card:"Card",bar:"Bar",line:"Line",segment:"Segment"}[r]}`,{self:{barColor:C,closeIconColor:O,closeIconColorHover:U,closeIconColorPressed:j,tabColor:K,tabBorderColor:oe,paneTextColor:At,tabFontWeight:Lt,tabBorderRadius:Et,tabFontWeightActive:Wt,colorSegment:It,fontWeightStrong:Ot,tabColorSegment:Dt,closeSize:jt,closeIconSize:Ht,closeColorHover:Mt,closeColorPressed:Nt,closeBorderRadius:Ft,[ae("panePadding",t)]:ye,[ae("tabPadding",i)]:Vt,[ae("tabPaddingVertical",i)]:Ut,[ae("tabGap",i)]:Gt,[ae("tabGap",`${i}Vertical`)]:Xt,[ae("tabTextColor",r)]:qt,[ae("tabTextColorActive",r)]:Kt,[ae("tabTextColorHover",r)]:Yt,[ae("tabTextColorDisabled",r)]:Jt,[ae("tabFontSize",t)]:Qt},common:{cubicBezierEaseInOut:Zt}}=g.value;return{"--n-bezier":Zt,"--n-color-segment":It,"--n-bar-color":C,"--n-tab-font-size":Qt,"--n-tab-text-color":qt,"--n-tab-text-color-active":Kt,"--n-tab-text-color-disabled":Jt,"--n-tab-text-color-hover":Yt,"--n-pane-text-color":At,"--n-tab-border-color":oe,"--n-tab-border-radius":Et,"--n-close-size":jt,"--n-close-icon-size":Ht,"--n-close-color-hover":Mt,"--n-close-color-pressed":Nt,"--n-close-border-radius":Ft,"--n-close-icon-color":O,"--n-close-icon-color-hover":U,"--n-close-icon-color-pressed":j,"--n-tab-color":K,"--n-tab-font-weight":Lt,"--n-tab-font-weight-active":Wt,"--n-tab-padding":Vt,"--n-tab-padding-vertical":Ut,"--n-tab-gap":Gt,"--n-tab-gap-vertical":Xt,"--n-pane-padding-left":Ce(ye,"left"),"--n-pane-padding-right":Ce(ye,"right"),"--n-pane-padding-top":Ce(ye,"top"),"--n-pane-padding-bottom":Ce(ye,"bottom"),"--n-font-weight-strong":Ot,"--n-tab-color-segment":Dt}}),Je=p?ft("tabs",re(()=>`${S.value[0]}${e.type[0]}`),Ye,e):void 0;return{mergedClsPrefix:d,mergedValue:z,renderedNames:new Set,segmentCapsuleElRef:be,tabsPaneWrapperRef:ge,tabsElRef:w,selfElRef:H,barElRef:x,addTabInstRef:F,xScrollInstRef:T,scrollWrapperElRef:M,addTabFixed:fe,tabWrapperStyle:Y,handleNavResize:Rt,mergedSize:S,handleScroll:Tt,handleTabsResize:kt,cssVars:p?void 0:Ye,themeClass:Je?.themeClass,animationDirection:Ge,renderNameListRef:Ue,yScrollElRef:P,handleSegmentResize:Bt,onAnimationBeforeLeave:gt,onAnimationEnter:mt,onAnimationAfterEnter:xt,onRender:Je?.onRender,startReachedRef:_,endReachedRef:W,isOverflow:We,handleButtonClick:wt,mergedTheme:g,rtlEnabled:n,mergedPlacement:h,...Pt}},render(){const{mergedClsPrefix:e,type:o,mergedPlacement:d,addTabFixed:p,addable:f,mergedSize:$,renderNameListRef:n,onRender:h,paneWrapperClass:g,paneWrapperStyle:w,startReachedRef:H,endReachedRef:x,isOverflow:M,showScrollButton:F,handleButtonClick:T,mergedTheme:P,rtlEnabled:_,$slots:{default:W,prefix:Z,suffix:S}}=this;h?.();const v=W?ke(W()).filter(L=>L.type.__TAB_PANE__===!0):[],A=W?ke(W()).filter(L=>L.type.__TAB__===!0):[],z=!A.length,I=o==="card",Y=o==="segment",V=!I&&!Y&&this.justifyContent;n.value=[];const ne=()=>{const L=(l(),m("div",{style:te(this.tabWrapperStyle),class:R(`${e}-tabs-wrapper`)},[V?y(()=>null):(l(),m("div",{key:1,class:R(`${e}-tabs-scroll-padding`),style:te(d==="top"||d==="bottom"?{width:`${this.tabsPadding}px`}:{height:`${this.tabsPadding}px`})},null,6)),z?(l(),m(ee,{key:2},[y(()=>v.map((J,ce)=>(n.value.push(J.props.name),Me((l(),q(Fe,Te(J.props,{internalCreatedByPane:!0,internalLeftPadded:ce!==0&&(!V||V==="center"||V==="start"||V==="end")}),Ze(J.children?{default:J.children.tab}:void 0),1040,["internalLeftPadded"]))))))],64)):(l(),m(ee,{key:3},[y(()=>A.map((J,ce)=>(n.value.push(J.props.name),Me(ce!==0&&!V?dt(J):J))))],64)),!p&&f&&I?(l(),m(ee,{key:4},[y(()=>it(f,(z?v.length:A.length)!==0))],64)):y(()=>null),V?y(()=>null):(l(),m("div",{key:7,class:R(`${e}-tabs-scroll-padding`),style:te({width:`${this.tabsPadding}px`})},null,6)),I?y(()=>null):(l(),m("div",{key:9,ref:"barElRef",class:R(`${e}-tabs-bar`)},null,2))],6));return l(),m("div",{ref:"tabsElRef",class:R(`${e}-tabs-nav-scroll-content`)},[I&&f?(l(),q(Ie,{key:0,onResize:this.handleTabsResize},{default:()=>L},1032,["onResize"])):(l(),m(ee,{key:1},[y(()=>L)],64)),I?(l(),m("div",{key:2,class:R(`${e}-tabs-pad`)},null,2)):y(()=>null)],2)},Q=Y?"top":d;return l(),m("div",{ref:"selfElRef",class:R([`${e}-tabs`,this.themeClass,`${e}-tabs--${o}-type`,`${e}-tabs--${$}-size`,V&&`${e}-tabs--flex`,`${e}-tabs--${Q}`,_&&`${e}-tabs--rtl`]),style:te(this.cssVars)},[E("div",{class:R([`${e}-tabs-nav--${o}-type`,`${e}-tabs-nav--${Q}`,`${e}-tabs-nav`])},[y(()=>Qe(Z,L=>L&&(l(),m("div",{class:R(`${e}-tabs-nav__prefix`)},[y(()=>L)],2)))),Y?(l(),q(Ie,{key:0,onResize:this.handleSegmentResize},{default:()=>(l(),m("div",{class:R(`${e}-tabs-rail`),ref:"tabsElRef"},[E("div",{class:R(`${e}-tabs-capsule`),ref:"segmentCapsuleElRef"},[E("div",{class:R(`${e}-tabs-wrapper`)},[E("div",{class:R(`${e}-tabs-tab`)},null,2)],2)],2),z?(l(),m(ee,{key:0},[y(()=>v.map((L,J)=>(n.value.push(L.props.name),l(),q(Fe,Te(L.props,{internalCreatedByPane:!0,internalLeftPadded:J!==0}),Ze(L.children?{default:L.children.tab}:void 0),1040,["internalLeftPadded"]))))],64)):(l(),m(ee,{key:1},[y(()=>A.map((L,J)=>(n.value.push(L.props.name),J===0?L:dt(L))))],64))],2))},1032,["onResize"])):(l(),m(ee,{key:1},[y(()=>F&&M&&(l(),q(st,{mergedClsPrefix:e,type:"prev",vertical:Q==="left"||Q==="right",disabled:H,rtl:!!_,theme:P.peers.Button,themeOverrides:P.peerOverrides.Button,onClick:T},null,8,["mergedClsPrefix","vertical","disabled","rtl","theme","themeOverrides","onClick"]))),(l(),q(Ie,{onResize:this.handleNavResize},{default:()=>(l(),m("div",{class:R(`${e}-tabs-nav-scroll-wrapper`),ref:"scrollWrapperElRef"},[["top","bottom"].includes(Q)?(l(),q(er,{key:0,ref:"xScrollInstRef",onScroll:this.handleScroll},{default:ne},1032,["onScroll"])):(l(),m("div",{key:1,class:R(`${e}-tabs-nav-y-scroll`),onScroll:this.handleScroll,ref:"yScrollElRef"},[y(()=>ne())],42,["onScroll"]))],2))},1032,["onResize"])),y(()=>F&&M&&(l(),q(st,{mergedClsPrefix:e,type:"next",vertical:Q==="left"||Q==="right",disabled:x,rtl:!!_,theme:P.peers.Button,themeOverrides:P.peerOverrides.Button,onClick:T},null,8,["mergedClsPrefix","vertical","disabled","rtl","theme","themeOverrides","onClick"])))],64)),p&&f&&I?(l(),m(ee,{key:2},[y(()=>it(f,!0))],64)):y(()=>null),y(()=>Qe(S,L=>L&&(l(),m("div",{class:R(`${e}-tabs-nav__suffix`)},[y(()=>L)],2))))],2),y(()=>z&&(this.animated&&(Q==="top"||Q==="bottom")?(l(),m("div",{key:1,ref:"tabsPaneWrapperRef",style:te(w),class:R([`${e}-tabs-pane-wrapper`,g])},[y(()=>lt(v,this.mergedValue,this.renderedNames,this.onAnimationBeforeLeave,this.onAnimationEnter,this.onAnimationAfterEnter,this.animationDirection))],6)):lt(v,this.mergedValue,this.renderedNames)))],6)}});function lt(e,o,d,p,f,$,n){const h=[];return e.forEach(g=>{const{name:w,displayDirective:H,"display-directive":x}=g.props,M=T=>H===T||x===T,F=o===w;if(g.key!==void 0&&(g.key=w),F||M("show")||M("show:lazy")&&d.has(w)){d.has(w)||d.add(w);const T=!M("if");h.push(T?ma(g,[[ya,F]]):g)}}),n?(l(),q(_a,{name:`${n}-transition`,onBeforeLeave:p,onEnter:f,onAfterEnter:$},{default:()=>h},1032,["name","onBeforeLeave","onEnter","onAfterEnter"])):h}function it(e,o){return l(),q(Fe,{ref:"addTabInstRef",key:"__addable",name:"__addable",internalCreatedByPane:!0,internalAddable:!0,internalLeftPadded:o,disabled:typeof e=="object"&&e.disabled},null,8,["internalLeftPadded","disabled"])}function dt(e){const o=Ca(e);return o.props?o.props.internalLeftPadded=!0:o.props={internalLeftPadded:!0},o}function Me(e){return Array.isArray(e.dynamicProps)?e.dynamicProps.includes("internalLeftPadded")||e.dynamicProps.push("internalLeftPadded"):e.dynamicProps=["internalLeftPadded"],e}const hr={class:"breadcrumb","aria-label":"Breadcrumb"},gr={class:"muted"},mr={class:"page-head"},xr={class:"page-head__title"},yr={class:"mono"},_r={class:"mono"},Cr={class:"mono"},wr={class:"mono"},Sr={style:{"margin-top":"12px"}},Rr={class:"mono"},zr={class:"mono"},kr={class:"mono"},$r=le({__name:"ServerDetailPage",setup(e){const o=Ra(),d=Sa(),p=ka(),f=Oa(),$=re(()=>String(o.params.id??"")),n=N(null),h=N(!1),g=N(null),w=N(!1),H=N(!1),x=N("overview"),M=re(()=>{const v=(n.value?.name??"").replace(/[^A-Za-z0-9]/g,"");return v.length>=2?v.slice(0,2).toUpperCase():v.length===1?v.toUpperCase():"ND"}),F=re(()=>n.value?[`${n.value.ip}:${n.value.port}`,n.value.os??"Unknown OS",n.value.arch??"Unknown arch",n.value.docker_version??"Docker unknown"].join(" · "):"");function T(S){return S??"—"}function P(S){return S==null?"—":`${Math.round(S)}%`}async function _(){if(!$.value){g.value="Unknown server.";return}h.value=!0,g.value=null;try{n.value=await Wa($.value)}catch(S){n.value=null,g.value=De(S)}finally{h.value=!1}}async function W(){if(n.value){w.value=!0;try{const S=await f.validate(n.value.id);if(S.server&&(n.value=S.server),S.ok){p.success(`${n.value.name}: validation passed`);return}const v=S.checks.filter(A=>!A.ok).map(A=>A.name).join(", ");p.error(S.message||`${n.value.name}: failed checks: ${v}`)}catch(S){p.error(De(S))}finally{w.value=!1}}}async function Z(){if(!n.value)return;const S=n.value.name;H.value=!0;try{await f.removeServer(n.value.id),p.success(`Deleted ${S}`),await d.push({name:"servers"})}catch(v){p.error(De(v))}finally{H.value=!1}}return ue($,()=>{x.value="overview",_()}),vt(()=>{_()}),(S,v)=>(l(),q(s(Se),{vertical:"",size:16},{default:b(()=>[E("nav",hr,[c(s(Oe),{to:"/servers"},{default:b(()=>[...v[1]||(v[1]=[D("Servers",-1)])]),_:1}),v[2]||(v[2]=E("span",{class:"breadcrumb__sep"},"/",-1)),E("span",gr,B(n.value?.name??$.value),1)]),c(s(Ba),{show:h.value},{default:b(()=>[g.value?(l(),q(s(ea),{key:0,type:"error","show-icon":!0,style:{"margin-bottom":"12px"}},{default:b(()=>[D(B(g.value),1)]),_:1})):et("",!0),n.value?(l(),m(ee,{key:1},[E("div",mr,[c(s(ta),{round:"",size:48},{default:b(()=>[D(B(M.value),1)]),_:1}),E("div",xr,[c(s(Se),{align:"center",size:10},{default:b(()=>[c(s(Re),{strong:"",style:{"font-size":"20px"}},{default:b(()=>[D(B(n.value.name),1)]),_:1}),c(Ta,{status:n.value.status,size:"medium"},null,8,["status"])]),_:1}),c(s(Re),{depth:"3"},{default:b(()=>[D(B(F.value),1)]),_:1})]),c(s(Se),{class:"page-head__actions",align:"center",size:8},{default:b(()=>[c(s(he),{loading:w.value,onClick:W},{default:b(()=>[...v[3]||(v[3]=[D(" Validate ",-1)])]),_:1},8,["loading"]),c(s(Oe),{to:{name:"server-containers",params:{id:n.value.id}},custom:""},{default:b(({navigate:A})=>[c(s(he),{type:"primary",onClick:A},{default:b(()=>[...v[4]||(v[4]=[D(" Open containers ",-1)])]),_:1},8,["onClick"])]),_:1},8,["to"])]),_:1})]),c(s(vr),{value:x.value,"onUpdate:value":v[0]||(v[0]=A=>x.value=A),type:"line",animated:""},{default:b(()=>[c(s(ve),{name:"overview",tab:"Overview"},{default:b(()=>[c(s(Se),{vertical:"",size:16,style:{"margin-top":"16px"}},{default:b(()=>[c(s(de),{title:"Node info"},{default:b(()=>[c(s(ot),{column:2,bordered:"","label-placement":"left"},{default:b(()=>[c(s(G),{label:"Name"},{default:b(()=>[D(B(n.value.name),1)]),_:1}),c(s(G),{label:"Address"},{default:b(()=>[E("span",yr,B(n.value.ip)+":"+B(n.value.port),1)]),_:1}),c(s(G),{label:"SSH user"},{default:b(()=>[E("span",_r,B(n.value.ssh_user),1)]),_:1}),c(s(G),{label:"Node ID"},{default:b(()=>[E("span",Cr,B(T(n.value.node_id)),1)]),_:1}),c(s(G),{label:"OS"},{default:b(()=>[D(B(T(n.value.os)),1)]),_:1}),c(s(G),{label:"Architecture"},{default:b(()=>[D(B(T(n.value.arch)),1)]),_:1}),c(s(G),{label:"Docker"},{default:b(()=>[D(B(T(n.value.docker_version)),1)]),_:1}),c(s(G),{label:"SSH key"},{default:b(()=>[E("span",wr,B(T(n.value.ssh_key_id)),1)]),_:1}),c(s(G),{label:"CPU usage"},{default:b(()=>[D(B(P(n.value.cpu_usage)),1)]),_:1}),c(s(G),{label:"Memory usage"},{default:b(()=>[D(B(P(n.value.mem_usage)),1)]),_:1}),c(s(G),{label:"Disk usage"},{default:b(()=>[D(B(P(n.value.disk_usage)),1)]),_:1}),c(s(G),{label:"Containers"},{default:b(()=>[D(B(n.value.container_count??"—"),1)]),_:1}),c(s(G),{label:"Last seen"},{default:b(()=>[D(B(s(ze)(n.value.last_seen)),1)]),_:1}),c(s(G),{label:"Registered"},{default:b(()=>[D(B(s(ze)(n.value.created_at)),1)]),_:1})]),_:1})]),_:1}),c(s(de),{title:"Labels"},{default:b(()=>[c(s(_e),{description:"No labels on this node yet."})]),_:1}),c(s(de),{title:"Danger zone"},{default:b(()=>[c(s(Re),{depth:"3"},{default:b(()=>[...v[5]||(v[5]=[D(" Deleting a node removes it from the control plane only. Containers, volumes and certificates on the machine are kept. ",-1)])]),_:1}),E("div",Sr,[c(s(Pa),{"positive-button-props":{type:"error"},onPositiveClick:Z},{trigger:b(()=>[c(s(he),{type:"error",ghost:"",loading:H.value},{default:b(()=>[...v[6]||(v[6]=[D(" Delete node ",-1)])]),_:1},8,["loading"])]),default:b(()=>[D(' Delete server "'+B(n.value.name)+'"? ',1)]),_:1})])]),_:1})]),_:1})]),_:1}),c(s(ve),{name:"containers",tab:"Containers"},{default:b(()=>[c(s(de),{style:{"margin-top":"16px"}},{default:b(()=>[c(s(_e),{description:"Container management lives on the containers page."},{extra:b(()=>[c(s(Oe),{to:{name:"server-containers",params:{id:n.value.id}},custom:""},{default:b(({navigate:A})=>[c(s(he),{type:"primary",onClick:A},{default:b(()=>[...v[7]||(v[7]=[D(" Open containers ",-1)])]),_:1},8,["onClick"])]),_:1},8,["to"])]),_:1})]),_:1})]),_:1}),c(s(ve),{name:"metrics",tab:"Metrics"},{default:b(()=>[c(s(de),{style:{"margin-top":"16px"}},{default:b(()=>[c(s(_e),{description:"Metrics ship in Phase 8."})]),_:1})]),_:1}),c(s(ve),{name:"proxy",tab:"Proxy & Traefik"},{default:b(()=>[c(s(de),{style:{"margin-top":"16px"}},{default:b(()=>[c(s(_e),{description:"Proxy & Traefik ships in Phase 6."})]),_:1})]),_:1}),c(s(ve),{name:"settings",tab:"Node settings"},{default:b(()=>[c(s(de),{title:"Node settings",style:{"margin-top":"16px"}},{default:b(()=>[c(s(Re),{depth:"3",style:{display:"block","margin-bottom":"12px"}},{default:b(()=>[...v[8]||(v[8]=[D(" Editable settings do not exist in the backend yet. Values below are read-only. ",-1)])]),_:1}),c(s(ot),{column:2,bordered:"","label-placement":"left"},{default:b(()=>[c(s(G),{label:"Name"},{default:b(()=>[D(B(n.value.name),1)]),_:1}),c(s(G),{label:"Address"},{default:b(()=>[E("span",Rr,B(n.value.ip)+":"+B(n.value.port),1)]),_:1}),c(s(G),{label:"SSH user"},{default:b(()=>[E("span",zr,B(n.value.ssh_user),1)]),_:1}),c(s(G),{label:"SSH key"},{default:b(()=>[E("span",kr,B(T(n.value.ssh_key_id)),1)]),_:1}),c(s(G),{label:"Registered"},{default:b(()=>[D(B(s(ze)(n.value.created_at)),1)]),_:1}),c(s(G),{label:"Updated"},{default:b(()=>[D(B(s(ze)(n.value.updated_at)),1)]),_:1})]),_:1})]),_:1})]),_:1})]),_:1},8,["value"])],64)):et("",!0)]),_:1},8,["show"])]),_:1}))}}),Nr=Da($r,[["__scopeId","data-v-b793a0e4"]]);export{Nr as default};
