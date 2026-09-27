import{M as ue,bU as ve,bR as r,O as be,a7 as v,am as f,aw as z,N as I,d as Q,P as Z,H as q,o as x,c as B,G as p,E as k,a as j,l as fe,aE as pe,a1 as A,Q as ke,U as me,aa as xe,y as O,q as ye,V as Pe,ab as d,aK as Se,aV as G,ah as ze,ak as Ie,a0 as Be,I as $e,bb as m}from"./index-BrC0X5ap.js";function He(e){const{textColor2:a,primaryColorHover:g,primaryColorPressed:b,primaryColor:l,infoColor:i,successColor:n,warningColor:s,errorColor:t,baseColor:C,borderColor:$,opacityDisabled:H,tagColor:R,closeIconColor:y,closeIconColorHover:P,closeIconColorPressed:o,borderRadiusSmall:c,fontSizeMini:u,fontSizeTiny:h,fontSizeSmall:w,fontSizeMedium:M,heightMini:_,heightTiny:E,heightSmall:T,heightMedium:W,closeColorHover:F,closeColorPressed:V,buttonColor2Hover:L,buttonColor2Pressed:N,fontWeightStrong:U}=e;return{...ve,closeBorderRadius:c,heightTiny:_,heightSmall:E,heightMedium:T,heightLarge:W,borderRadius:c,opacityDisabled:H,fontSizeTiny:u,fontSizeSmall:h,fontSizeMedium:w,fontSizeLarge:M,fontWeightStrong:U,textColorCheckable:a,textColorHoverCheckable:a,textColorPressedCheckable:a,textColorChecked:C,colorCheckable:"#0000",colorHoverCheckable:L,colorPressedCheckable:N,colorChecked:l,colorCheckedHover:g,colorCheckedPressed:b,border:`1px solid ${$}`,textColor:a,color:R,colorBordered:"rgb(250, 250, 252)",closeIconColor:y,closeIconColorHover:P,closeIconColorPressed:o,closeColorHover:F,closeColorPressed:V,borderPrimary:`1px solid ${r(l,{alpha:.3})}`,textColorPrimary:l,colorPrimary:r(l,{alpha:.12}),colorBorderedPrimary:r(l,{alpha:.1}),closeIconColorPrimary:l,closeIconColorHoverPrimary:l,closeIconColorPressedPrimary:l,closeColorHoverPrimary:r(l,{alpha:.12}),closeColorPressedPrimary:r(l,{alpha:.18}),borderInfo:`1px solid ${r(i,{alpha:.3})}`,textColorInfo:i,colorInfo:r(i,{alpha:.12}),colorBorderedInfo:r(i,{alpha:.1}),closeIconColorInfo:i,closeIconColorHoverInfo:i,closeIconColorPressedInfo:i,closeColorHoverInfo:r(i,{alpha:.12}),closeColorPressedInfo:r(i,{alpha:.18}),borderSuccess:`1px solid ${r(n,{alpha:.3})}`,textColorSuccess:n,colorSuccess:r(n,{alpha:.12}),colorBorderedSuccess:r(n,{alpha:.1}),closeIconColorSuccess:n,closeIconColorHoverSuccess:n,closeIconColorPressedSuccess:n,closeColorHoverSuccess:r(n,{alpha:.12}),closeColorPressedSuccess:r(n,{alpha:.18}),borderWarning:`1px solid ${r(s,{alpha:.35})}`,textColorWarning:s,colorWarning:r(s,{alpha:.15}),colorBorderedWarning:r(s,{alpha:.12}),closeIconColorWarning:s,closeIconColorHoverWarning:s,closeIconColorPressedWarning:s,closeColorHoverWarning:r(s,{alpha:.12}),closeColorPressedWarning:r(s,{alpha:.18}),borderError:`1px solid ${r(t,{alpha:.23})}`,textColorError:t,colorError:r(t,{alpha:.1}),colorBorderedError:r(t,{alpha:.08}),closeIconColorError:t,closeIconColorHoverError:t,closeIconColorPressedError:t,closeColorHoverError:r(t,{alpha:.12}),closeColorPressedError:r(t,{alpha:.18})}}const Re={common:ue,self:He};var we={color:Object,type:{type:String,default:"default"},round:Boolean,size:String,closable:Boolean,disabled:{type:Boolean,default:void 0}},Me=be("tag",`
 --n-close-margin: var(--n-close-margin-top) var(--n-close-margin-right) var(--n-close-margin-bottom) var(--n-close-margin-left);
 white-space: nowrap;
 position: relative;
 box-sizing: border-box;
 cursor: default;
 display: inline-flex;
 align-items: center;
 flex-wrap: nowrap;
 padding: var(--n-padding);
 border-radius: var(--n-border-radius);
 color: var(--n-text-color);
 background-color: var(--n-color);
 transition: 
 border-color .3s var(--n-bezier),
 background-color .3s var(--n-bezier),
 color .3s var(--n-bezier),
 box-shadow .3s var(--n-bezier),
 opacity .3s var(--n-bezier);
 line-height: 1;
 height: var(--n-height);
 font-size: var(--n-font-size);
`,[v("strong",`
 font-weight: var(--n-font-weight-strong);
 `),f("border",`
 pointer-events: none;
 position: absolute;
 left: 0;
 right: 0;
 top: 0;
 bottom: 0;
 border-radius: inherit;
 border: var(--n-border);
 transition: border-color .3s var(--n-bezier);
 `),f("icon",`
 display: flex;
 margin: 0 4px 0 0;
 color: var(--n-text-color);
 transition: color .3s var(--n-bezier);
 font-size: var(--n-avatar-size-override);
 `),f("avatar",`
 display: flex;
 margin: 0 6px 0 0;
 `),f("close",`
 margin: var(--n-close-margin);
 transition:
 background-color .3s var(--n-bezier),
 color .3s var(--n-bezier);
 `),v("round",`
 padding: 0 calc(var(--n-height) / 3);
 border-radius: calc(var(--n-height) / 2);
 `,[f("icon",`
 margin: 0 4px 0 calc((var(--n-height) - 8px) / -2);
 `),f("avatar",`
 margin: 0 6px 0 calc((var(--n-height) - 8px) / -2);
 `),v("closable",`
 padding: 0 calc(var(--n-height) / 4) 0 calc(var(--n-height) / 3);
 `)]),v("icon, avatar",[v("round",`
 padding: 0 calc(var(--n-height) / 3) 0 calc(var(--n-height) / 2);
 `)]),v("disabled",`
 cursor: not-allowed !important;
 opacity: var(--n-opacity-disabled);
 `),v("checkable",`
 cursor: pointer;
 box-shadow: none;
 color: var(--n-text-color-checkable);
 background-color: var(--n-color-checkable);
 `,[z("disabled",[I("&:hover","background-color: var(--n-color-hover-checkable);",[z("checked","color: var(--n-text-color-hover-checkable);")]),I("&:active","background-color: var(--n-color-pressed-checkable);",[z("checked","color: var(--n-text-color-pressed-checkable);")])]),v("checked",`
 color: var(--n-text-color-checked);
 background-color: var(--n-color-checked);
 `,[z("disabled",[I("&:hover","background-color: var(--n-color-checked-hover);"),I("&:active","background-color: var(--n-color-checked-pressed);")])])])]);const _e=["onClick","onMouseenter","onMouseleave"],Ee={...Z.props,...we,bordered:{type:Boolean,default:void 0},checked:Boolean,checkable:Boolean,strong:Boolean,triggerClickOnClose:Boolean,onClose:[Array,Function],onMouseenter:Function,onMouseleave:Function,"onUpdate:checked":Function,onUpdateChecked:Function,internalCloseFocusable:{type:Boolean,default:!0},internalCloseIsButtonTag:{type:Boolean,default:!0},onCheckedChange:Function},Te=ze("n-tag");var Le=Q({name:"Tag",props:Ee,slots:Object,setup(e){const a=ye(null),{mergedBorderedRef:g,mergedClsPrefixRef:b,inlineThemeDisabled:l,mergedRtlRef:i,mergedComponentPropsRef:n}=ke(e),s=O(()=>e.size||n?.value?.Tag?.size||"medium"),t=Z("Tag","-tag",Me,Re,e,b);Ie(Te,{roundRef:Be(e,"round")});function C(){if(!e.disabled&&e.checkable){const{checked:o,onCheckedChange:c,onUpdateChecked:u,"onUpdate:checked":h}=e;u&&u(!o),h&&h(!o),c&&c(!o)}}function $(o){if(e.triggerClickOnClose||o.stopPropagation(),!e.disabled){const{onClose:c}=e;c&&Pe(c,o)}}const H={setTextContent(o){const{value:c}=a;c&&(c.textContent=o)}},R=me("Tag",i,b),y=O(()=>{const{type:o,color:{color:c,textColor:u}={}}=e,h=s.value,{common:{cubicBezierEaseInOut:w},self:{padding:M,closeMargin:_,borderRadius:E,opacityDisabled:T,textColorCheckable:W,textColorHoverCheckable:F,textColorPressedCheckable:V,textColorChecked:L,colorCheckable:N,colorHoverCheckable:U,colorPressedCheckable:J,colorChecked:X,colorCheckedHover:Y,colorCheckedPressed:ee,closeBorderRadius:oe,fontWeightStrong:re,[d("colorBordered",o)]:ae,[d("closeSize",h)]:le,[d("closeIconSize",h)]:se,[d("fontSize",h)]:ce,[d("height",h)]:K,[d("color",o)]:ne,[d("textColor",o)]:te,[d("border",o)]:ie,[d("closeIconColor",o)]:D,[d("closeIconColorHover",o)]:de,[d("closeIconColorPressed",o)]:he,[d("closeColorHover",o)]:ge,[d("closeColorPressed",o)]:Ce}}=t.value,S=Se(_);return{"--n-font-weight-strong":re,"--n-avatar-size-override":`calc(${K} - 8px)`,"--n-bezier":w,"--n-border-radius":E,"--n-border":ie,"--n-close-icon-size":se,"--n-close-color-pressed":Ce,"--n-close-color-hover":ge,"--n-close-border-radius":oe,"--n-close-icon-color":D,"--n-close-icon-color-hover":de,"--n-close-icon-color-pressed":he,"--n-close-icon-color-disabled":D,"--n-close-margin-top":S.top,"--n-close-margin-right":S.right,"--n-close-margin-bottom":S.bottom,"--n-close-margin-left":S.left,"--n-close-size":le,"--n-color":c||(g.value?ae:ne),"--n-color-checkable":N,"--n-color-checked":X,"--n-color-checked-hover":Y,"--n-color-checked-pressed":ee,"--n-color-hover-checkable":U,"--n-color-pressed-checkable":J,"--n-font-size":ce,"--n-height":K,"--n-opacity-disabled":T,"--n-padding":M,"--n-text-color":u||te,"--n-text-color-checkable":W,"--n-text-color-checked":L,"--n-text-color-hover-checkable":F,"--n-text-color-pressed-checkable":V}}),P=l?xe("tag",O(()=>{let o="";const{type:c,color:{color:u,textColor:h}={}}=e;return o+=c[0],o+=s.value[0],u&&(o+=`a${G(u)}`),h&&(o+=`b${G(h)}`),g.value&&(o+="c"),o}),y,e):void 0;return{...H,rtlEnabled:R,mergedClsPrefix:b,contentRef:a,mergedBordered:g,handleClick:C,handleCloseClick:$,cssVars:l?void 0:y,themeClass:P?.themeClass,onRender:P?.onRender}},render(){const{mergedClsPrefix:e,rtlEnabled:a,closable:g,color:{borderColor:b}={},round:l,onRender:i,$slots:n}=this;i?.();const s=q(n.avatar,C=>C&&(x(),B("div",{class:k(`${e}-tag__avatar`)},[p(()=>C)],2))),t=q(n.icon,C=>C&&(x(),B("div",{class:k(`${e}-tag__icon`)},[p(()=>C)],2)));return x(),B("div",{class:k([`${e}-tag`,this.themeClass,{[`${e}-tag--rtl`]:a,[`${e}-tag--strong`]:this.strong,[`${e}-tag--disabled`]:this.disabled,[`${e}-tag--checkable`]:this.checkable,[`${e}-tag--checked`]:this.checkable&&this.checked,[`${e}-tag--round`]:l,[`${e}-tag--avatar`]:s,[`${e}-tag--icon`]:t,[`${e}-tag--closable`]:g}]),style:A(this.cssVars),onClick:this.handleClick,onMouseenter:this.onMouseenter,onMouseleave:this.onMouseleave},[p(()=>t||s),j("span",{class:k(`${e}-tag__content`),ref:"contentRef"},[p(()=>this.$slots.default?.())],2),!this.checkable&&g?(x(),fe(pe,{key:0,clsPrefix:e,class:k(`${e}-tag__close`),disabled:this.disabled,onClick:this.handleCloseClick,focusable:this.internalCloseFocusable,round:l,isButtonTag:this.internalCloseIsButtonTag,absolute:!0},null,8,["clsPrefix","class","disabled","onClick","focusable","round","isButtonTag"])):p(()=>null),!this.checkable&&this.mergedBordered?(x(),B("div",{key:2,class:k(`${e}-tag__border`),style:A({borderColor:b})},null,6)):p(()=>null)],46,_e)}}),Ne=Q({name:"ChevronRight",render(){return(()=>{const e=$e("6ab04425f4fcb756");return e[0]||(e[0]=j("svg",{viewBox:"0 0 16 16",fill:"none",xmlns:"http://www.w3.org/2000/svg"},[j("path",{d:"M5.64645 3.14645C5.45118 3.34171 5.45118 3.65829 5.64645 3.85355L9.79289 8L5.64645 12.1464C5.45118 12.3417 5.45118 12.6583 5.64645 12.8536C5.84171 13.0488 6.15829 13.0488 6.35355 12.8536L10.8536 8.35355C11.0488 8.15829 11.0488 7.84171 10.8536 7.64645L6.35355 3.14645C6.15829 2.95118 5.84171 2.95118 5.64645 3.14645Z",fill:"currentColor"})],-1))})()}});const We=45e3;async function Ue(){return(await m.get("/servers")).data.servers??[]}async function Oe(e){return(await m.post("/servers",e)).data.server}async function je(e){return(await m.get(`/servers/${e}`)).data.server}async function Ke(e){await m.delete(`/servers/${e}`)}async function De(e){const a=await m.post(`/servers/${e}/validate`,{},{timeout:We,validateStatus:g=>g===200||g===422});return{ok:a.status===200,checks:a.data.checks??[],server:a.data.server??null,message:a.data.message??""}}async function qe(e){return(await m.post("/private-keys",e)).data}function Fe(e){return typeof e=="object"&&e!==null&&"message"in e&&"status"in e}function Ae(e){return Fe(e)?e.message||"Request failed":e instanceof Error?e.message:"Something went wrong. Please try again."}export{Ne as C,Le as T,Oe as a,Ke as b,qe as c,Ae as d,je as g,Fe as i,Ue as l,Te as t,De as v};
